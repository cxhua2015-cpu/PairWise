# readyqueue405

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。队列使用显式非负单调逻辑时钟：`Apply` 原子顺序执行批次中的 `Enqueue`/`Cancel`，`Pop` 按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）原子取出 `ReadyAt <= now` 的任务。

## 架构与多文件联动

- `readyqueue405/prioritybox.go` — 核心事务引擎：类型、`New`、`Apply`、`Pop`、`Snapshot`。
- `readyqueue405/validation.go` — 无副作用批次预检 `ValidateBatch`，只做结构校验（时间非负、kind 合法、ID 字符集与字节上限、Cancel 负载为零），不读取可变状态。`Apply` 在任何状态读取之前调用同一套校验，保证预检与事务语义一致。
- `readyqueue405/stats.go` — 线性一致统计 `Stats`，在与事务相同的互斥锁下取快照，始终反映单一一致的历史点。
- `readyqueue405/clone.go` — 深拷贝 `Clone`，保留逻辑时钟（now、generation、nextRevision），与原队列完全隔离所有权。

## 索引

主索引是 `map[string]Item`，提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）。规范弹出顺序通过 `less` 比较器（Priority 降序 → ReadyAt 升序 → ID 升序）在 `Pop`/`Snapshot` 时对候选集排序获得；由于队列容量受 `Options.MaxItems` 上限约束，排序代价有界，无需维护额外的堆结构。

## 候选事务

`Apply` 先在候选副本（当前 item map 的浅拷贝，Item 为值类型故足够）上顺序执行全部操作并分配 revision，最后才做一次容量检查。任何失败（时间回退、重复、缺失、超容量）直接丢弃候选，时间、状态与 revision 计数天然回滚；成功时一次性提交并仅递增一次 generation。空批次是 no-op，不改变 generation。

## 所有权

所有公开方法（`Apply`/`Pop`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）都可并发调用，由单个 `sync.Mutex` 串行化。`Snapshot` 与 `Pop` 返回的切片均为新建副本，与内部状态隔离；`Clone` 复制全部 map 条目，两个队列之后互不可见。

## 复杂度

- `Apply`：O(n + k)，n 为当前元素数（候选拷贝），k 为批次数。
- `Pop`：O(n log n)，受 `MaxItems` 上限约束；删除为 O(k)。
- `Snapshot`/`Clone`：O(n log n) / O(n)。
- `Stats`/`ValidateBatch`：O(1) / O(k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
