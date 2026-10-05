# resourcelease199

并发安全的内存型“资源租约表 199”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Entry`，按键 O(1) 定位。
- `Snapshot` 与 `Expire` 的返回切片按键字典序排序，保证输出确定性。

**候选事务（Apply）**
1. 先对整个批次做完整结构校验（`Now >= 0`、kind 合法、键为 `[a-z0-9-_]` 且不超过 `MaxKeyBytes`），不读取任何状态。
2. 加锁后检查时间单调性（`Now < now` → `ErrTime`）。
3. 在候选副本（map 的浅拷贝）上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
4. 最终条目数超过 `MaxEntries` → `ErrCapacity`。
5. 任何错误都直接丢弃候选副本：淘汰、时间与 revision 一并回滚，无需补偿操作。
6. 成功则一次性提交副本，`Now` 前进，generation 恰好加一；空批次是完全无操作。

**所有权**
- 所有公开方法通过单个 `sync.Mutex` 串行化，可并发调用。
- `Snapshot.Entries` 与 `Expire` 返回的切片均为新分配的拷贝，调用方修改不会影响内部状态，反之亦然。

**复杂度**
- `Apply`：O(E + K)，E 为当前条目数（拷贝与淘汰扫描），K 为批次操作数。
- `Expire`：O(E + R log R)，R 为过期条目数（排序）。
- `Snapshot`：O(E log E)（排序）。
- 空间：O(E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
