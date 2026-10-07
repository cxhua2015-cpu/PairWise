# metacatalog396

并发安全的内存型元数据目录，实现见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

- **索引**：`Store` 内部以 `map[string]Record` 为主索引，按名称 O(1) 定位记录；另维护 `totalBytes` 累计值避免每次遍历求和。`Snapshot`/`Result.Changed` 在返回前对名称排序。
- **候选事务**：`Apply` 先在无锁状态下完成全部结构校验（kind、名称字符集与长度、Value 长度、Delete 不带 Value），再持写锁把当前索引克隆为候选副本，在副本上按输入顺序执行 Put/Delete。Put 分配连续 revision 并深拷贝 Value；Delete 不分配 revision，删除不存在的键返回 `ErrNotFound`。记录数与 Value 总字节容量只在批次末检查，任何失败直接丢弃候选副本，状态、generation、revision 全部不变（天然回滚）。
- **所有权**：写入时深拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为独立副本，返回值与内部状态完全隔离。
- **并发**：`sync.RWMutex` 保护全部状态，`Apply` 取写锁，`Get`/`Snapshot` 取读锁。
- **复杂度**：结构校验 O(批次总字节)；候选事务 O(n + b)，n 为当前记录数、b 为批次数；容量检查 O(1)；`Get` O(1)；`Snapshot` O(n log n)（排序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
