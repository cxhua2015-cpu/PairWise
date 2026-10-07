# 到期状态表 374 (expirytable374)

并发安全的内存型到期状态表，面向分布式控制面。Go 1.22+，仅依赖标准库。

## 设计

### 索引
主索引为 `map[string]Entry`，键到条目 O(1) 定位。条目按 `ExpiresAt <= Now`
的闭区间判定到期；`Apply` 与 `Expire` 复用同一边界。`Snapshot` 与 `Expire`
的结果按键名/到期时间排序，保证输出确定性，且返回的切片为独立副本，与内部
状态完全隔离（调用方修改不影响表）。

### 候选事务
`Apply` 采用候选状态（copy-on-write）事务模型：

1. **结构校验**：先完整校验整个批次（kind 合法、键字符集与长度、非负时间），
   不触碰任何状态，失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now` 必须非负且不小于表当前时间，否则 `ErrTime`。
3. **候选执行**：克隆当前条目为候选集，先删除 `ExpiresAt <= Now` 的条目，
   再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
4. **容量检查**：对最终候选集检查 `MaxEntries`，超限返回 `ErrCapacity`。
5. **提交**：仅在全部成功时一次性替换内部状态、推进时间、revision 计数器，
   且非空批次 generation 只加一；空批次不改变任何状态。

任何一步失败（`ErrNotFound`、`ErrCapacity` 等）直接丢弃候选集，淘汰、时间与
revision 随之一并回滚，表保持应用批次前的精确状态。

### 所有权
`Table` 内部状态（条目 map、时间、generation、revision 计数器）由一把
`sync.Mutex` 保护，绝不逃逸到返回值中。所有公开方法（`Apply`、`Expire`、
`Snapshot`）可安全并发调用；`Snapshot`/`Expire` 返回的切片与 Entry 均为新建
副本，调用方获得独占所有权。

### 复杂度
- `Apply`：O(n + m)，n 为当前条目数（候选克隆 + 到期扫描），m 为批次数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序输出）。
- 空间：O(n)。

## 测试

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
