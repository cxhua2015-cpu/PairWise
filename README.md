# taskqueue140

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

包内三个生产文件协同工作：

- `prioritybox.go` — 状态引擎 `Queue`：事务化 Apply、优先 Pop、快照。
- `policy.go` — 准入策略 `Policy`：独立同步的 actor 白名单与单批操作数上限。
- `coordinator.go` — 协调层 `Coordinator`：先授权、再调用引擎，并为成功、拒绝与引擎失败记录连续序号的审计日志。

## 索引

引擎用 `map[string]Item` 作为主索引，按 ID O(1) 定位任务；不存在独立的堆结构，Pop 与 Snapshot 时按需对候选集排序，排序键为 Priority 降序、ReadyAt 升序、ID 升序。

## 候选事务

`Apply` 先做整批结构校验（kind、ID 字符集与字节上限、非负时间），不读取任何状态；随后在锁内把 Enqueue/Cancel 暂存为变更列表（含批内 pending/removed 视图以正确处理同批先 Cancel 再 Enqueue 同一 ID），容量只在末尾检查一次。任一失败直接丢弃暂存变更，时间、状态、revision 与 generation 全部保持原值；成功才一次性提交。非空成功批次 generation 只加一，空批次不变。

## 所有权

- `Queue` 内部状态由一把 `sync.Mutex` 保护；`Snapshot` 与 `Pop` 返回的切片均为新建副本，不与内部存储共享。
- `Policy` 用独立的 `sync.RWMutex` 保护；`ReplaceActors` 先完整校验再原子整体替换白名单，失败时旧名单继续生效。
- `Coordinator` 用独立互斥锁维护审计序号与日志；`Decisions` 返回拷贝，调用方修改返回切片不影响内部日志。
- 策略拒绝发生在调用引擎之前，不读取也不修改核心状态。

## 复杂度

- `Apply`：O(k)，k 为批内操作数（另加 O(k) 的暂存空间）。
- `Pop`：O(n log n)，n 为当前任务数（筛选 ReadyAt <= now 后排序，取前 limit 个并原子删除）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- `Authorize` / `ReplaceActors`：O(1) / O(a)，a 为 actor 数量。
- `Coordinator.Apply`：在引擎开销上附加 O(1) 审计追加。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
