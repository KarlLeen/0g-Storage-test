# 0G Storage 文件上传下载演示

这是一个使用 0G Storage Go SDK 的演示程序，展示了如何将文件切分、上传到 0G Storage 网络，然后下载并验证。

## 功能特性

- ✅ 将 1KB 文件切分为 4 个 256B 的文件
- ✅ 使用 0g-storage-client 上传文件到 0G Storage 网络
- ✅ 下载文件并验证完整性
- ✅ 合并下载的文件并验证与原始文件一致
- ✅ 使用 Turbo Indexer
- ✅ 强制副本数为 1
- ✅ 地址验证功能

## 配置

在运行程序前，请修改 `main.go` 中的配置：

```go
const (
    EvmRPC     = "https://evmrpc-testnet.0g.ai"
    IndexerRPC = "https://indexer-storage-testnet-turbo.0g.ai"
    PrivateKey = "your private key"  // 请填入你的私钥
    UserAddress = "your address"     // 请填入你的以太坊地址
    ReplicaNum = 1                   // 强制副本数为 1
    
    // 文件配置
    TotalSize    = 1 * 1024  // 1KB
    FragmentSize = 256       // 256B
)
```

## 安装依赖

```bash
go get github.com/0gfoundation/0g-storage-client
```

## 运行程序

```bash
go run main.go
```

或者编译后运行：

```bash
go build -o 0gtest main.go
./0gtest
```

## 程序流程

1. **初始化客户端**：连接 Web3 和 Indexer 客户端
2. **地址验证**：验证私钥和地址是否匹配
3. **生成测试文件**：创建 1KB 的随机测试文件
4. **文件切分**：将文件切分为 4 个 256B 的碎片
5. **上传文件**：依次上传每个碎片到 0G Storage 网络
6. **下载验证**：根据 Root Hash 下载所有碎片
7. **文件验证**：验证下载的文件与原始文件是否一致
8. **文件合并**：合并所有下载的碎片并验证完整性

## 注意事项

- Turbo Indexer 的 HTTP GET 请求返回 404 是正常行为，不影响 SDK 使用
- 程序会自动尝试多种节点选择策略以确保成功
- 所有临时文件会在程序结束后自动清理

## 依赖

- Go 1.25.4+
- [0g-storage-client](https://github.com/0gfoundation/0g-storage-client) v1.2.1

## 许可证

MIT

