# readyqueue400

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）。
- 就绪选择不做额外堆索引：`Pop` 全量扫描并筛选 `ReadyAt <= now`，再按
  “Priority 降序、ReadyAt 升序、ID 升序” 排序取前 `limit` 个。
  容量受 `MaxItems` 上限约束，单次扫描成本可控，且避免了“堆顶未就绪但
  低优先级元素已就绪”时单堆无法直接服务的问题。

### 候选事务
- `Apply` 先做整批结构校验（kind、ID 字符集与字节上限），不读取任何状态；
  随后在临界区内顺序执行 Enqueue/Cancel，Enqueue 就地分配递增 revision。
- 执行期间记录撤销日志（undo log）：任一步失败或最终容量检查
  `len(items) > MaxItems` 失败时逆序回放，精确回滚条目、时间与 revision，
  队列保持批前状态（`Snapshot` 前后一致）。
- 容量只在批次末尾检查，因此同批 “Cancel 后 Enqueue” 可以成功。
- 非空成功批次 generation 恰好 +1；空批次不改变任何状态。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，
  可任意并发调用；`Pop` 的选择与删除在同一临界区内原子完成。
- 返回的 `[]Item`/`Snapshot.Items` 均为新分配的拷贝，调用方修改不影响内部状态。
- 时间为显式非负单调时钟：`now < 0` 为 `ErrInvalidInput`，`now` 小于当前
  队列时间为 `ErrTime`；成功的 `Apply`/`Pop` 推进队列时间。

### 复杂度
- `Apply`：O(k·n) 最坏（k 为批内 op 数，map 操作为均摊 O(1)，撤销日志 O(k)），
  结构校验 O(k·L)（L 为 ID 长度）。
- `Pop`：O(n log n)（扫描 O(n) + 排序），删除 O(min(limit, n))。
- `Snapshot`：O(n log n)（拷贝 + 规范序排序）。
- 空间：O(n)，n 为当前条目数（≤ MaxItems）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
