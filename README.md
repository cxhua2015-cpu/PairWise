# expirytable214

并发安全的内存型“到期状态表”，Go 1.22+，仅依赖标准库。语义见 `SPEC.md`。

## 索引与所有权

- 主索引为 `map[string]Entry`，键到条目一一对应；未维护额外的按过期时间排序的堆/树索引，淘汰与 `Expire` 通过全表扫描完成。
- 表独占其内部 map；`Snapshot` 与 `Expire` 返回的切片均为新建拷贝，调用方修改不影响内部状态（所有权不共享）。
- 所有公开方法通过单个 `sync.Mutex` 串行化，支持任意并发调用。

## 候选事务（Apply）

1. 先对整个批次做结构校验（时间非负、kind 合法、键合法），不读状态。
2. 加锁后检查单调时间（`Now >= 当前 now`）。
3. 在候选 map 副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
4. 校验最终容量；任何错误（`ErrNotFound`/`ErrCapacity` 等）直接丢弃候选，淘汰、时间与 revision 一并回滚。
5. 非空成功批次 generation 恰好加一；空批次不改变任何状态。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`）删除并返回条目，同时推进表时间。

## 复杂度

- `Apply`：O(E + B)，E 为当前条目数（候选拷贝与淘汰扫描），B 为批内操作数。
- `Expire`：O(E + K log K)，K 为到期条目数（结果按键排序，保证确定性输出）。
- `Snapshot`：O(E log E)（排序输出）。
- 空间：O(E)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
