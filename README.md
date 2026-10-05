# taskqueue160

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：队列内部使用 `map[string]Item` 作为唯一索引，按 ID O(1) 定位，
用于 Enqueue 的重复检测（`ErrExists`）与 Cancel 的删除（`ErrNotFound`）。
不维护堆：Pop 与 Snapshot 的规范顺序（Priority 降序、ReadyAt 升序、ID 升序）
通过每次读取时对候选切片排序获得，实现简单且天然避免堆与 map 双索引的一致性问题。

**候选事务**：`Apply` 先在不上锁的情况下完成整批结构校验（kind、ID 字符集与
字节上限、非负时间），再加锁做单调时间检查。非空批次在 `items` 的克隆
（候选 map）上顺序执行 Enqueue/Cancel，revision 在候选上递增；容量
（`MaxItems`）只在所有操作执行完后做最终检查。任一步失败直接丢弃候选，
时间、状态、revision、generation 全部保持原值，实现原子回滚；成功则整体
提交，generation 恰好加一，revision 取最后一次 Enqueue 分配的值。空批次
不改变任何状态，generation 不变。

**所有权**：所有公开方法由单个 `sync.Mutex` 保护，可并发调用。`Snapshot`
与 `Pop` 返回的切片均为新分配的副本，调用方修改返回值不会影响内部状态；
内部也绝不保留调用方传入的切片。

**复杂度**（n = 队列中任务数，k = 批内操作数，m = 就绪任务数，l = Pop 上限）：
- `New`：O(1)
- `Apply`：结构校验 O(k·ID长度)；候选克隆 O(n)；执行 O(k)；总计 O(n + k)
- `Pop`：筛选 O(n)，排序 O(m log m)，删除 O(l)；总计 O(n + m log m)
- `Snapshot`：O(n log n)（拷贝并排序）

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
