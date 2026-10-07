# balanceledger302

并发安全的内存型余额账本，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账本主体是 `map[string]Account`，按账户名 O(1) 定位。`Top` 与
  `Snapshot` 不维护额外的有序结构，而是在读取时把 map 值拷贝成切片后排序
  （`Top` 按值降序、名称升序；`Snapshot` 按名称升序），以简单换取写入路径的
  O(1) 与实现的可审计性。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在
  写锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete。
  溢出与绝对值上限在每次算术前检查，账户容量仅在批次末对候选表检查。任一
  失败直接丢弃候选并返回错误，原状态未被触碰，天然整体回滚；全部成功才一次
  性提交并递增 generation。revision 由单调计数器在 Add/Set 时连续分配。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.
  Accounts`）都是新建并填充的副本，调用方修改不会影响内部状态；`Account`
  为纯值类型，map 浅拷贝即完成状态隔离。
- **并发**：单把 `sync.RWMutex`。`Apply` 持写锁，`Top`/`Snapshot` 持读锁，
  允许并发只读。
- **复杂度**：`Apply` 为 O(n + a)，n 为批内操作数、a 为当前账户数（候选拷
  贝）；`Top` 与 `Snapshot` 为 O(a log a)；空间 O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
