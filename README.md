# prioritybox

并发安全的内存型优先级信箱（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**所有权与并发**
- `Queue` 内部由一把 `sync.Mutex` 保护，所有公开方法（`Apply`/`Pop`/`Snapshot`）可并发调用。
- 队列独占其内部 `map[string]Item`；`Item` 为纯值类型，`Snapshot`/`Pop` 返回的切片均为新建副本，调用方修改返回值不会影响内部状态。
- 时钟为显式非负单调时间：`Apply.Now` 与 `Pop` 的 `now` 都不得小于当前时钟（否则 `ErrTime`），成功后推进时钟。

**索引**
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于存在性判断（`ErrExists`/`ErrNotFound`）与 Cancel 删除。
- 堆序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需对候选切片排序实现，不为每次写入维护有序结构，写入保持 O(1)。

**候选事务**
- `Apply` 先对整个批次做纯结构校验（kind、ID 字符集与长度、ReadyAt 非负），不读取任何状态。
- 校验通过后在原 map 的克隆（候选状态）上顺序执行 Enqueue/Cancel，revision 从候选计数器分配；容量（`MaxItems`）只在全部操作执行完后检查一次。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选：时间、条目与 revision 计数器全部保持原值，generation 不变。非空成功批次提交候选并将 generation 加一；空批次只推进时钟，generation 不变。

**复杂度**（n = 队列中条目数，b = 批次大小，k = 弹出数量）
- `Apply`：结构校验 O(b)，候选克隆 O(n + b)，执行 O(b)，总计 O(n + b)。
- `Pop`：筛选就绪项 O(n)，排序 O(n log n)，删除 O(k)。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
