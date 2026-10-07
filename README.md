# balanceledger312

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：唯一索引是 `map[string]Account`（按名称）。`Top` 与 `Snapshot` 在读取时对快照切片即时排序（分别为值降序/名称升序、名称升序），不维护额外的有序结构，以换取写入路径 O(1)。
- **候选事务**：`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在持锁状态下把当前 map 克隆为候选副本，按输入顺序在副本上执行 Add/Set/Delete；Add/Set 在副本上分配连续 revision。任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本，实现整体回滚；仅在全部成功且批次末账户数不超过容量时才用副本替换正式状态，并将 generation 加一。
- **溢出与上限**：所有加减在执行前先做 int64 溢出检测，输入与结果的绝对值都不得越过 `MaxAbsValue`（`math.MinInt64` 无合法绝对值，直接拒绝）。
- **所有权**：`Account` 为纯值类型；`Top`/`Snapshot`/`Result.Changed` 返回的都是新分配的切片与副本，调用方修改返回值不影响内部状态。
- **并发**：单个 `sync.Mutex` 保护全部内部状态，所有公开方法可并发调用；`Snapshot` 不返回错误，空批次成功且不改变 generation。
- **复杂度**：设批次长度为 B、账户数为 N。`Apply` 为 O(N + B)（克隆 + 逐 op O(1)）；`Top(k)` 为 O(N log N)；`Snapshot` 为 O(N log N)；空间 O(N)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
