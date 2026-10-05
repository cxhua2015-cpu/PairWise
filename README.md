# taskqueue125

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 `Enqueue` 的重复检测与 `Cancel` 删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久有序结构，而是在 `Pop`/`Snapshot` 时对候选集即时排序，保证map 迭代顺序不影响结果。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与长度、ReadyAt 非负），再检查时间单调性，然后在**候选副本**（map 的浅拷贝）上顺序执行 Enqueue/Cancel 并分配 revision。
- 容量只在末尾检查一次；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选副本，时间、状态、revision 计数器全部不变，实现原子回滚。
- 成功时一次性提交：替换 map、推进 `now`、generation 恰好加一；空批次为无操作。

### 所有权与并发
- 所有公开方法由单个 `sync.Mutex` 串行化，任意 goroutine 可并发调用。
- `Pop`/`Snapshot` 返回的切片与 `Item` 值均为新分配的副本，调用方修改不会影响内部状态；内部不保留调用方传入的切片。

### 复杂度
- `New`：O(1)。
- `Apply`（k 个 op、当前 n 项）：O(n + k)，候选 map 拷贝 + 逐 op O(1)。
- `Pop`（r 个就绪项）：O(n + r log r)，扫描 + 排序 + 删除。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
