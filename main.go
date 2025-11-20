package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	// 使用 0gfoundation 作为导入路径
	"github.com/0gfoundation/0g-storage-client/common/blockchain"
	"github.com/0gfoundation/0g-storage-client/indexer"
	"github.com/0gfoundation/0g-storage-client/transfer"
)

const (
	// 网络配置 (使用文档提供的测试网地址)
	EvmRPC      = "https://evmrpc-testnet.0g.ai"
	IndexerRPC  = "https://indexer-storage-testnet-turbo.0g.ai" // 使用 Turbo Indexer（HTTP GET 返回 404 是正常的）
	PrivateKey  = "your private key"                            // <--- 请在此处填入你的私钥
	UserAddress = "your address"                                // 你的以太坊地址
	ReplicaNum  = 1                                             // 强制副本数为 1

	// 文件配置
	TotalSize    = 1 * 1024 // 1KB
	FragmentSize = 256      // 256B (物理切分大小，1KB = 4 * 256B)
)

func main() {
	ctx := context.Background()

	// ==========================================
	// 1. 初始化客户端 (Web3 和 Indexer)
	// ==========================================
	fmt.Println(">>> 正在初始化 0G 客户端...")

	// 初始化 Web3 客户端 (用于发送交易)
	w3client := blockchain.MustNewWeb3(EvmRPC, PrivateKey)
	defer w3client.Close()

	// 从私钥计算地址
	privateKeyBytes, err := hexutil.Decode("0x" + PrivateKey)
	if err != nil {
		log.Printf("警告：无法解析私钥: %v", err)
	} else {
		privateKey, err := crypto.ToECDSA(privateKeyBytes)
		if err != nil {
			log.Printf("警告：无法转换私钥: %v", err)
		} else {
			publicKey := privateKey.Public()
			publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
			if !ok {
				log.Printf("警告：无法获取公钥")
			} else {
				derivedAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
				fmt.Printf(">>> 从私钥派生的地址: %s\n", derivedAddress.Hex())
				fmt.Printf(">>> 你提供的地址:     %s\n", UserAddress)
				if derivedAddress.Hex() == common.HexToAddress(UserAddress).Hex() {
					fmt.Println(">>> ✓ 地址匹配！")
				} else {
					fmt.Println(">>> ⚠ 地址不匹配，请检查私钥是否正确")
				}
				fmt.Println()
			}
		}
	}

	// 初始化 Indexer 客户端 (用于查找存储节点)
	// 使用 Turbo Indexer（注意：Turbo Indexer 的 HTTP GET 请求返回 404 是正常行为，不影响 SDK 使用）
	fmt.Println(">>> 正在连接 Turbo Indexer...")
	indexerClient, err := indexer.NewClient(IndexerRPC)
	if err != nil {
		log.Fatalf("Indexer 初始化失败: %v", err)
	}
	fmt.Println(">>> ✓ 已连接到 Turbo Indexer")

	// ==========================================
	// 2. 生成并切分文件
	// ==========================================
	workDir := "temp_data"
	os.MkdirAll(workDir, 0755)
	defer os.RemoveAll(workDir) // 程序结束后清理临时文件

	largeFileName := filepath.Join(workDir, "large_data_1kb.bin")
	fmt.Printf(">>> 生成 1KB 测试文件: %s\n", largeFileName)
	if err := generateRandomFile(largeFileName, TotalSize); err != nil {
		log.Fatal(err)
	}

	fmt.Println(">>> 正在将 1KB 文件切分为 4 个 256B 的碎片...")
	fragments, err := splitFile(largeFileName, FragmentSize, workDir)
	if err != nil {
		log.Fatal(err)
	}

	// ==========================================
	// 3. 循环上传分片
	// ==========================================
	var rootHashes []string // 存储每个分片的 Root Hash 用于下载

	// 获取存储节点
	// SelectNodes 参数: context, expectedReplica, dropped, method, fullTrusted
	// 强制使用副本数为 1（ReplicaNum = 1）
	fmt.Printf(">>> 正在选择存储节点（副本数: %d，使用 Turbo Indexer）...\n", ReplicaNum)
	var selectedNodes *transfer.SelectedNodes

	// 尝试多种参数组合以确保能够选择到节点
	// 策略1: 使用信任节点，method 为空
	selectedNodes, err = indexerClient.SelectNodes(ctx, ReplicaNum, nil, "", true)
	if err != nil {
		fmt.Printf(">>> 策略1失败: %v\n", err)

		// 策略2: 使用混合节点，method 为空
		fmt.Println(">>> 尝试策略2: 使用混合节点...")
		selectedNodes, err = indexerClient.SelectNodes(ctx, ReplicaNum, nil, "", false)
		if err != nil {
			fmt.Printf(">>> 策略2失败: %v\n", err)

			// 策略3: 使用信任节点，method 为 "random"
			fmt.Println(">>> 尝试策略3: 使用信任节点，method=random...")
			selectedNodes, err = indexerClient.SelectNodes(ctx, ReplicaNum, nil, "random", true)
			if err != nil {
				fmt.Printf(">>> 策略3失败: %v\n", err)

				// 策略4: 使用混合节点，method 为 "random"
				fmt.Println(">>> 尝试策略4: 使用混合节点，method=random...")
				selectedNodes, err = indexerClient.SelectNodes(ctx, ReplicaNum, nil, "random", false)
				if err != nil {
					log.Fatalf("所有节点选择策略都失败，最后一个错误: %v", err)
				}
			}
		}
	}

	// 合并信任节点和发现节点用于下载
	allNodes := append(selectedNodes.Trusted, selectedNodes.Discovered...)
	if len(allNodes) == 0 {
		log.Fatalf("错误：没有可用的存储节点")
	}
	fmt.Printf(">>> 已选择 %d 个存储节点 (信任: %d, 发现: %d)\n",
		len(allNodes), len(selectedNodes.Trusted), len(selectedNodes.Discovered))

	for i, fragPath := range fragments {
		fmt.Printf("\n--- [上传任务 %d/4] 处理文件: %s ---\n", i+1, filepath.Base(fragPath))

		// 创建上传器
		uploader, err := transfer.NewUploader(ctx, w3client, selectedNodes)
		if err != nil {
			log.Printf("创建上传器失败: %v", err)
			continue
		}

		// 执行上传
		// UploadFile 返回: 交易哈希(TxHash), Merkle根(RootHash), 错误
		// 注意：根据文档，UploadFile 会自动处理文件分块，fragment size 由 SDK 内部管理
		txHash, rootHash, err := uploader.UploadFile(ctx, fragPath)
		if err != nil {
			log.Printf("上传失败: %v", err)
			continue
		}

		fmt.Printf(">>> 上传成功!\n")
		fmt.Printf("    Tx Hash: %s\n", txHash.String())
		fmt.Printf("    Root Hash: %s\n", rootHash.String())

		rootHashes = append(rootHashes, rootHash.String())
	}

	// ==========================================
	// 4. 下载验证
	// ==========================================
	fmt.Println("\n>>> 开始下载验证...")

	// 使用合并后的节点列表创建下载器
	downloader, err := transfer.NewDownloader(allNodes)
	if err != nil {
		log.Fatalf("创建下载器失败: %v", err)
	}

	var downloadedFiles []string
	for i, root := range rootHashes {
		downloadPath := filepath.Join(workDir, fmt.Sprintf("downloaded_part_%d.bin", i))
		fmt.Printf("正在下载分片 %d (Root: %s)...\n", i+1, root)

		// Download 参数: context, rootHash, outputPath, verifyProof(true)
		err := downloader.Download(ctx, root, downloadPath, true)
		if err != nil {
			log.Printf("下载失败: %v", err)
			continue
		}

		fmt.Printf("    下载完成 -> %s\n", downloadPath)
		downloadedFiles = append(downloadedFiles, downloadPath)

		// 验证下载的文件是否与原始分片一致
		originalPath := fragments[i]
		if filesEqual(originalPath, downloadPath) {
			fmt.Printf("    ✓ 验证通过：下载文件与原始分片一致\n")
		} else {
			fmt.Printf("    ✗ 验证失败：下载文件与原始分片不一致\n")
		}
	}

	// ==========================================
	// 5. 合并下载的文件
	// ==========================================
	if len(downloadedFiles) > 0 {
		fmt.Println("\n>>> 正在合并下载的文件...")
		mergedPath := filepath.Join(workDir, "merged_1kb.bin")
		if err := mergeFiles(downloadedFiles, mergedPath); err != nil {
			log.Printf("合并文件失败: %v", err)
		} else {
			fmt.Printf(">>> 合并完成: %s\n", mergedPath)

			// 验证合并后的文件是否与原始文件一致
			if filesEqual(largeFileName, mergedPath) {
				fmt.Printf(">>> ✓ 验证通过：合并文件与原始1KB文件完全一致！\n")
			} else {
				fmt.Printf(">>> ✗ 验证失败：合并文件与原始文件不一致\n")
			}
		}
	}

	fmt.Println("\n>>> 所有演示任务完成！")
}

// --- 辅助函数：生成随机文件 ---
func generateRandomFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// 快速生成：写入随机种子后补零（为了演示速度），或者使用 io.CopyN(f, rand.Reader, size) 生成全随机数据
	_, err = io.CopyN(f, rand.Reader, size)
	return err
}

// --- 辅助函数：切分文件 ---
func splitFile(srcPath string, chunkSize int64, outDir string) ([]string, error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var chunkPaths []string
	buffer := make([]byte, chunkSize)

	for i := 0; ; i++ {
		bytesRead, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if bytesRead == 0 {
			break
		}

		chunkName := filepath.Join(outDir, fmt.Sprintf("part_%d.bin", i))
		err = os.WriteFile(chunkName, buffer[:bytesRead], 0644)
		if err != nil {
			return nil, err
		}
		chunkPaths = append(chunkPaths, chunkName)
	}
	return chunkPaths, nil
}

// --- 辅助函数：比较两个文件是否相同 ---
func filesEqual(file1, file2 string) bool {
	f1, err := os.Open(file1)
	if err != nil {
		return false
	}
	defer f1.Close()

	f2, err := os.Open(file2)
	if err != nil {
		return false
	}
	defer f2.Close()

	// 比较文件大小
	stat1, _ := f1.Stat()
	stat2, _ := f2.Stat()
	if stat1.Size() != stat2.Size() {
		return false
	}

	// 逐字节比较
	buf1 := make([]byte, 32*1024) // 32KB 缓冲区
	buf2 := make([]byte, 32*1024)

	for {
		n1, err1 := f1.Read(buf1)
		n2, err2 := f2.Read(buf2)

		if err1 == io.EOF && err2 == io.EOF {
			return true
		}

		if err1 != nil || err2 != nil {
			return false
		}

		if n1 != n2 {
			return false
		}

		for i := 0; i < n1; i++ {
			if buf1[i] != buf2[i] {
				return false
			}
		}
	}
}

// --- 辅助函数：合并多个文件 ---
func mergeFiles(filePaths []string, outputPath string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	for i, filePath := range filePaths {
		inFile, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("无法打开文件 %s: %v", filePath, err)
		}

		written, err := io.Copy(outFile, inFile)
		if err != nil {
			inFile.Close()
			return fmt.Errorf("复制文件 %s 失败: %v", filePath, err)
		}
		inFile.Close()

		fmt.Printf("    已合并分片 %d: %d 字节\n", i+1, written)
	}

	return nil
}
