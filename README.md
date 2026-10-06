# readyqueue260

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构与索引

实现分布在四个联动文件中：

- `readyqueue260/prioritybox.go` — 核心事务引擎。主索引为 `map[string]Item`（按 ID 哈希，
  Enqueue/Cancel 的存在性判断为 O(1)）；`generation`、`nextRevision`、`now` 三个逻辑时钟
  与同一把 `sync.Mutex` 保护，所有公开方法串行化，天然并发安全。
- `readyqueue260/validation.go` — 无副作用的批次结构预检 `ValidateBatch`。`Apply` 复用同一套
  校验函数（kind 合法、ID 字符集/字节上限、ReadyAt 非负、Cancel 不携带额外负载），
  保证预检与事务结构语义完全一致；预检不读取队列状态。
- `readyqueue260/stats.go` — `Stats` 在同一把锁内读取计数器，是线性一致的状态摘要。
- `readyqueue260/clone.go` — `Clone` 在锁内深拷贝 map 与全部逻辑时钟。

## 候选事务（Apply）

`Apply` 先完整结构校验，再检查时间单调性，然后按顺序执行 Enqueue/Cancel：

- Enqueue 分配单调递增的 revision；Cancel 删除已有项。
- 容量上限只在批次末尾检查一次（允许“先删后增”的净不变批次）。
- 每一步都记录 undo journal；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）即逆序回滚
  items、`nextRevision` 与 `now`，批次原子生效或完全无副作用。
- 非空成功批次 `generation` 恰好加一；空批次不改变任何状态。

## 所有权

- `Snapshot`/`Pop` 返回的切片均为新建拷贝，与内部状态隔离，调用方可自由持有/修改。
- `Clone` 返回完全独立的深拷贝（含逻辑时钟），克隆体与原队列不共享任何内存，
  双方后续事务互不影响。

## 复杂度

- `ValidateBatch`：O(L)，L 为批次内 op 总数；不触碰状态。
- `Apply`：O(L)（map 操作均摊 O(1)），回滚同为 O(L)。
- `Pop`：O(N log N)，N 为当前就绪项数（扫描 + 规范序排序 + 批量删除）。
- `Snapshot`：O(N log N)；`Stats`：O(1)；`Clone`：O(N)。

规范顺序：Priority 降序、ReadyAt 升序、ID 升序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
