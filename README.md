# notificationqueue

并发安全的内存型通知优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）。
- 就绪集合不维护额外堆结构：`Pop`/`Snapshot` 时把 map 物化为切片后按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems` 上限约束，
  单次排序成本有界，换来实现的简单与无索引不一致风险。

### 候选事务
- `Apply` 先做整批结构校验（`Now >= 0`、kind 合法、ID 字符集与字节上限、
  `ReadyAt >= 0`），不读取任何状态；随后检查时间单调性（`ErrTime`）。
- 非空批次在克隆出的候选 map 上顺序执行 Enqueue/Cancel，revision 从
  `nextRevision` 起局部递增；仅在全部操作成功且**最终**容量不超 `MaxItems`
  时才一次性提交（map、now、nextRevision、generation）。任何失败直接丢弃候选，
  时间、状态、revision、generation 天然回滚，无需反向补偿。
- 空批次为无操作：不推进时间、不增加 generation，返回当前代与最近 revision。

### 所有权与并发
- 所有公开方法经同一把 `sync.Mutex` 串行化；`Apply` 的校验—执行—提交与
  `Pop` 的选择—删除都是原子临界区，并发 Pop 不会重复交付同一任务。
- 返回的 `[]Item` 与 `Snapshot.Items` 均为新建切片与值拷贝，调用方修改
  不影响内部状态；`Item` 为纯值类型，无共享指针。
- `Pop` 不推进队列时钟，仅按参数 `now` 过滤 `ReadyAt <= now` 并原子删除。

### 复杂度
设 n 为队列中任务数（n ≤ MaxItems），b 为批次操作数：
- `New`：O(1)。
- `Apply`：O(n + b)（克隆候选 map + 顺序执行），校验 O(b·L)，L 为 ID 长度。
- `Pop`：O(n log n)（物化 + 排序），删除 O(min(limit, 就绪数))。
- `Snapshot`：O(n log n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
