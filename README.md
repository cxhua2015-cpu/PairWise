# resourceledger187

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

- **索引**：账本主体为 `map[string]Account`，按名称 O(1) 定位账户；无有序索引，`Top`/`Snapshot` 在读取时物化并排序，写路径保持 O(1)。
- **候选事务**：`Apply` 分两段执行。第一段仅做结构校验（kind、名称字符集与字节上限），不读状态；第二段在账户表的浅拷贝（候选事务）上按输入顺序执行 Add/Set/Delete，Add/Set 依次分配连续 revision，溢出在算术前检测，绝对值上限逐 op 执行，账户容量仅在批次末检查。任一步失败直接丢弃候选，原状态零改动，实现整体回滚；成功才一次性提交并将 generation 加一（空批次不加）。
- **所有权**：`Ledger` 内部状态由单个 `sync.Mutex` 保护，所有公开方法可并发调用。返回的 `[]Account`（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为每次调用新建并复制的切片，调用方修改不影响内部状态；`Changed` 只包含批次结束时仍存在的账户。
- **复杂度**：设批次长度 k、账户数 n。`Apply` 时间 O(k + n)（候选拷贝），空间 O(n)；`Top(m)` 时间 O(n log n)，返回前 m 条（数值降序、名称升序）；`Snapshot` 时间 O(n log n)（按名称排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
