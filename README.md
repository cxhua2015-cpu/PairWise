# resourcelease164

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即租约名，Put/Touch/Delete 均为 O(1) 均摊。
- 淘汰采用全量扫描（O(n)），未维护额外的按过期时间排序的堆；在“批次驱动、显式时间”的模型下，这是简单且正确的取舍。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

### 候选事务（candidate transaction）
`Apply` 分两阶段：
1. **校验**：先对整个批次做结构校验（kind、键字符集与字节上限、非负 ExpiresAt），再检查时间单调性（`Now >= 0 && Now >= 当前时间`），任一失败直接返回，状态不变。
2. **候选执行**：在条目映射的副本上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete，Put/Touch 从候选 revision 计数器分配版本。最终容量超限或任何错误（`ErrNotFound`、`ErrCapacity`）都会丢弃整个候选状态——淘汰、时间与 revision 一并回滚。只有全部成功才提交：替换映射、推进时间、非空批次 generation 恰好加一。

### 所有权
- 表独占其内部条目映射；`Snapshot().Entries` 与 `Expire` 的返回值都是新建切片与值拷贝，调用方可自由修改，不影响内部状态。
- 传入的 `Batch`/`Op` 仅按值读取，实现不保留其引用。

### 并发
所有公开方法由单一 `sync.Mutex` 保护，可安全并发调用；`Apply`/`Expire` 串行化，时间因此天然单调。

### 复杂度
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
