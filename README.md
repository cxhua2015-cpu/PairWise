# taskqueue155

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：队列内部使用 `map[string]Item` 作为唯一所有权索引，按 ID O(1) 定位任务，
用于 Enqueue 的重复检测与 Cancel 的删除。Pop/Snapshot 的规范顺序
（Priority 降序、ReadyAt 升序、ID 升序）在读取时对待命中项即时排序得到，
不为排序单独维护堆结构，避免双索引不一致风险。

**候选事务**：`Apply` 先在候选副本（克隆的 map 与标量字段）上顺序执行整批
Enqueue/Cancel，最终容量只在末尾检查；任一步失败直接丢弃候选，队列的时间、
状态、revision、generation 全部保持原值，天然实现回滚。全部成功才一次性提交。
批次先做完整结构校验（kind、ID 字符集与长度、ReadyAt 非负），再读取任何状态。

**所有权**：所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，
可并发调用；`Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态。
时间为显式非负单调时钟：`Apply.Now` 与 `Pop` 的 now 不得回退（`ErrTime`），
负值与非法 limit 返回 `ErrInvalidInput`。

**复杂度**（n 为队列长度，b 为批次大小，k 为就绪任务数）：
- `New`：O(1)
- `Apply`：O(n + b)（克隆 + 顺序执行），容量检查 O(1)
- `Pop`：O(n + k log k)（筛选就绪 + 排序 + 删除）
- `Snapshot`：O(n log n)

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
