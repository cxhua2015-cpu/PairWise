# expirytable319

并发安全的内存型“到期状态表 319”，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，按键提供 O(1) 的 Put/Touch/Delete 查找。
- 不维护额外的堆/有序索引：`Snapshot` 与 `Expire` 的返回在临界区内按键排序（规范顺序），代价为 O(n log n)，换取实现的简单与无额外写路径开销。

### 候选事务
- `Apply` 先完整结构校验（kind、键字符集与字节上限、非负时间），再检查单调时间（`Now < now` 返回 `ErrTime`）。
- 通过后在与原状态隔离的候选 map 上执行：先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 从单调计数器分配 revision。
- 任何错误（`ErrNotFound`、最终容量 `ErrCapacity` 等）发生时直接丢弃候选，淘汰、时间与 revision 一并回滚，原状态零副作用。
- 全部成功才一次性提交：替换 map、推进 `now` 与 `nextRevision`，非空批次 `generation` 恰好加一，空批次不变。
- `Expire` 使用相同闭区间边界（`ExpiresAt <= now`），推进时间并返回被淘汰条目。

### 所有权
- 所有公开方法由单一 `sync.Mutex` 保护，可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不影响内部状态；`Entry`/`Result`/`Snapshot` 均为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(n + k)，n 为当前条目数（候选复制与淘汰扫描），k 为批内操作数。
- `Expire`：O(n + m log m)，m 为被淘汰条目数（排序返回）。
- `Snapshot`：O(n log n)（排序输出）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
