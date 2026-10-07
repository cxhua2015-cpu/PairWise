# balanceledger322

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot` 在读取时把 map 物化为切片并排序（分别为值降序/名称升序、名称升序），不维护额外的有序结构，以换取写入路径 O(1)。
- **候选事务**：`Apply` 先在持锁状态下把当前 map 浅拷贝为候选副本，所有 Add/Set/Delete 及溢出、绝对值上限、容量检查都在副本上进行；任一步失败直接丢弃副本，实现整体回滚，成功时一次性换入并推进 `generation` 与 `nextRevision`。批次在读取状态前先完成整批结构校验（kind、名称字符集与长度）。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新分配的拷贝，调用方修改不会影响内部状态；内部状态仅通过 `Ledger` 方法访问。
- **并发**：单把 `sync.Mutex` 保护全部内部状态，公开方法可安全并发调用；写操作串行化，保证 revision 连续分配且 generation 每非空成功批次只增一次。

## 复杂度

- `Apply`：O(n + a)，n 为批内操作数，a 为当前账户数（候选副本克隆）。
- `Top(k)`：O(a log a)，返回前 k 个。
- `Snapshot`：O(a log a)。
- 空间：O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
