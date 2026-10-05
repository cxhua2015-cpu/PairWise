# heartbeat

并发安全的内存型节点心跳表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Entry`，键即节点名，Put/Touch/Delete 与存在性判断均为 O(1) 均摊。
- 过期边界（`ExpiresAt <= Now`，闭区间）通过候选状态上的线性扫描求值；`Expire` 与 `Snapshot` 的结果分别按 `(ExpiresAt, Key)` 与 `Key` 排序后返回，保证确定性输出。

**候选事务**
- `Apply` 先在持有锁的情况下对整个批次做结构校验（kind、键字符集与字节上限、非负 `ExpiresAt`），再检查时间单调性（`Now` 不得回退）。
- 校验通过后，在克隆出的候选 map 上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
- 任何错误（`ErrNotFound`、`ErrCapacity` 等）发生时直接丢弃候选：淘汰、时间与 revision 分配一并回滚，已发布状态不受影响。只有全部成功才一次性提交候选 map、时间、revision 计数器，非空批次 generation 恰好加一，空批次不变。

**所有权**
- 所有公开方法（`Apply`/`Expire`/`Snapshot`）由单一 `sync.Mutex` 保护，可任意并发调用。
- 返回的切片（`Expire` 的淘汰列表、`Snapshot.Entries`）均为新分配的副本，调用方修改不会污染内部状态；`Entry`/`Result`/`Snapshot` 均按值返回。

**复杂度**（n = 当前条目数，m = 批次内操作数）
- `Apply`：O(n + m) 时间与 O(n) 额外空间（候选克隆）。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot`：O(n log n)（复制并按键排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
