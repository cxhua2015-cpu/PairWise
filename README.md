# tokenledger

并发安全的内存型令牌计数账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Ledger` 内部以 `map[string]Account` 作为主索引，按名称 O(1) 定位账户；revision 与 generation 为两个单调计数器。`Top` 与 `Snapshot` 在读取时临时物化并排序，不维护额外的有序结构，因此写入路径保持 O(1) 摊销。

**候选事务**：`Apply` 先做整批结构校验（kind 合法、名称合法），不触碰状态；随后在索引的克隆副本（候选事务）上按输入顺序执行 Add/Set/Delete，Add/Set 在副本上分配连续 revision。任何一步失败（`ErrNotFound`/`ErrValue`/`ErrCapacity`）直接丢弃副本，原状态零改动，天然实现整体回滚；全部成功且批次末账户数不超过容量时，才用副本原子替换主索引并将 generation 加一。空批次不推进 generation。

**所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新分配的副本，调用方可自由修改，不会影响账本内部状态；内部也绝不持有调用方传入的切片。

**并发**：单把 `sync.RWMutex` 保护全部状态。`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可多读者并行。

**复杂度**（n = 账户数，k = 批次内 op 数）：
- `Apply`：时间 O(n + k)（克隆索引 + 逐 op O(1)），空间 O(n)。
- `Top`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
