# resourceledger127

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账本核心为 `map[string]Account`（名称 → 账户），提供 O(1) 的点查/写入。
`Top` 与 `Snapshot` 在读取时把 map 物化为新切片并排序，不维护有序索引——
写路径保持 O(1)，排序成本只在读路径支付。

**候选事务**：`Apply` 分两阶段。第一阶段在加锁前做完整结构校验（kind、名称
字符集与字节上限、delta/value 绝对值上限）。第二阶段在写锁内把当前账户表复制为
候选副本，按输入顺序执行 Add/Set/Delete：Add/Set 从单调递增计数器分配连续
revision，加法前先检测 int64 溢出再执行算术，账户容量上限仅在批次末检查。
任一步失败直接丢弃候选副本，实现整体回滚；成功时一次性换入候选副本并使
generation 递增一次（空批次不变）。

**所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）
都是新分配的副本，与内部状态完全隔离；调用方修改返回值不影响账本。
并发安全由一把 `sync.RWMutex` 保证：`Apply` 持写锁，`Top`/`Snapshot` 持读锁。

**复杂度**（n = 账户数，k = 批次数，m = Top 请求数）：
- `Apply`：O(n + k)，候选复制 O(n)，逐 op O(1)。
- `Top`：O(n log n)，返回前 m 名（值降序、名称升序）。
- `Snapshot`：O(n log n)，按名称升序。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
