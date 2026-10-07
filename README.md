# metacatalog431

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配；完整结构校验先于状态读取，记录数与 Value 总字节容量只在批次末检查；任何失败整体回滚状态、generation 与 revision。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`Store`（`sync.RWMutex` + 内部 `state`）、`New/Apply/Get/Snapshot`，以及共享的 `state.applyBatch` 事务实现。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套 `validateBatch` 结构语义（名称字符集/长度、Value 上限、Delete 不得携带 Value、未知 kind）。
- `stats.go` — 线性一致统计：`Stats` 在读锁下汇总 generation、nextRevision、记录数与 Value 总字节。
- `clone.go` — 深拷贝：`Clone` 保留逻辑时钟（generation/nextRevision）并完全隔离所有权。
- `preview.go` — 事务预演：`Preview` 在一次线性化快照上克隆候选状态并复用 `applyBatch`，返回候选 `Result/Snapshot/Stats`；错误及优先级与同一状态上的 `Apply` 完全一致，失败时全部返回零值，原对象与逻辑时钟不变。

## 索引

记录存储为 `map[string]Record` 主索引，按名称 O(1) 定位；`Snapshot`/`Result.Changed` 在返回前按名称排序。`totalValueBytes` 作为冗余聚合随每次 Put/Delete 增量维护，使容量检查与 `Stats` 均为 O(1)。

## 候选事务

`Apply` 与 `Preview` 都先在 `state.cloneState()` 生成的候选状态上顺序执行整批操作，批次末统一做容量检查；只有成功时才把候选状态原子换入（`Apply`）或仅用于读取结果（`Preview`）。因此失败天然零副作用，无需显式回滚日志。

## 所有权

所有跨越 API 边界的 `[]byte`（入参 Value、`Get`/`Snapshot`/`Result`/`Clone` 的返回值）都经过深拷贝，调用方与内部状态、原对象与克隆/候选状态之间互不共享内存。

## 复杂度

- `Get` / `Stats`：O(1)；`ValidateBatch`：O(B)，B 为批次操作数。
- `Apply` / `Preview`：O(R + B + C log C)，R 为当前记录数（候选克隆），C 为批次触及的名称数（排序）。
- `Snapshot` / `Clone`：O(R log R) / O(R)。
- 空间：O(R + 每批候选副本 O(R))。
