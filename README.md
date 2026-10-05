# taskqueue200

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引

队列内部维护两份互为冗余的结构，由同一把 `sync.Mutex` 保护：

- `byID map[string]Item`：按 ID 的所有权索引，Enqueue 的重复检测与
  Cancel 的存在性检查都是 O(1)。
- `sorted []Item`：按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）
  维护的有序切片。Enqueue 通过二分查找定位插入点，Cancel 通过二分
  查找定位删除点；Snapshot 直接复制该切片即为规范顺序。

### 候选事务（Apply）

`Apply` 分两阶段：

1. **结构校验**：先校验 `Now >= 0`、所有 Op 的 kind 合法、ID 符合
   字符集与长度上限；此阶段不读取任何队列状态。空批次在校验后
   直接成功返回，不改变 generation、时间或状态。
2. **候选执行**：克隆 `byID` 与 `sorted` 得到候选状态，按顺序执行
   Enqueue（分配递增 revision，从 1 开始）/ Cancel；任何一步失败
   （`ErrExists`/`ErrNotFound`）或最终容量检查失败（`ErrCapacity`）
   都直接丢弃候选状态——时间、条目与 revision 计数器天然回滚，
   无需补偿日志。只有全部成功且最终容量不超限才一次性提交：
   时钟前进到 `Batch.Now`、generation 恰好加一、候选状态生效。

### 所有权与并发

所有公开方法（`Apply`/`Pop`/`Snapshot`）在同一把互斥锁下执行，因此
批次之间是线性化的；`Pop` 的选择与删除原子完成。`Item` 为值类型，
`Snapshot` 与 `Pop` 返回的切片都是新分配的副本，调用方修改返回值
不会影响队列内部状态。时钟为显式非负单调时间：`now < 0` 返回
`ErrInvalidInput`，`now` 小于当前时钟返回 `ErrTime`，失败的调用
不会移动时钟。

### 复杂度

设 n 为队列中条目数，b 为批次大小：

- `New`：O(1)。
- `Apply`：结构校验 O(b·L)（L 为 ID 长度）；候选克隆 O(n)；
  每个 Enqueue/Cancel 为 O(log n) 定位 + O(n) 切片搬移，总计
  O(n + b·n)；最终容量检查 O(1)。
- `Pop`：O(n) 扫描选出就绪条目（候选按规范顺序产出，无需再排序），
  删除为 O(n)。
- `Snapshot`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
