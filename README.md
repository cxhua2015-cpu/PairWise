# metacatalog436

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构

实现按职责拆分为五个文件：

- `servicecatalog.go` — 核心事务引擎：`Store`、`New`、`Apply`、`Get`、`Snapshot`，以及无锁内部事务函数 `applyInner`。
- `validation.go` — 无副作用的结构预检：`ValidateBatch` 与 `validateBatch`/`validateName`，是 `Apply`、`ValidateBatch`、`Preview` 共享的唯一结构语义来源。
- `stats.go` — 线性一致的 `Stats` 统计（generation、next revision、记录数、Value 总字节）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）的完全独立深拷贝 `Clone`。
- `preview.go` — 事务预演 `Preview`：在一次读锁内克隆出候选 Store（线性化点），在候选上运行与 `Apply` 完全相同的 `applyInner`，返回候选 `Result`、`Snapshot`、`Stats`；原对象的状态、generation、revision 与逻辑时钟均不变，失败时返回与 `Apply` 相同的错误且全部返回值为零值。

## 索引

`Store` 使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`generation` 与 `nextRevision` 作为逻辑时钟随结构存储。`Snapshot`/`Result.Changed` 在返回前按名称排序，不维护额外的有序结构。

## 候选事务与回滚

`Apply` 在写锁内先完整结构校验（不读状态），再在索引的浅拷贝上按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查。任一步失败直接丢弃拷贝，原索引、generation、revision 原样保留，实现零成本回滚。`Preview` 复用同一 `applyInner`，因此错误及优先级（`ErrInvalidInput` → `ErrNotFound` → `ErrCapacity`）与同一状态上的 `Apply` 完全一致。

## 所有权

存入的 Value 在 Put 时深拷贝；内部存储的切片安装后永不变更，因此浅拷贝索引可安全共享底层切片。`Get`、`Snapshot`、`Result.Changed`、`Clone` 返回的所有切片均为新分配的深拷贝，调用方修改不会影响 Store，Store 的后续变更也不会影响已返回的数据。`Clone`/`Preview` 产生的候选对象与原对象无任何共享可变状态。

## 并发

所有公开方法通过 `sync.RWMutex` 保护：读路径（`Get`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch` 之外的只读操作）使用读锁，`Apply` 使用写锁，`Preview` 的线性化点是克隆时的一次读锁。

## 复杂度

- `Get`：O(1) 平均（加返回值拷贝 O(v)，v 为 Value 长度）。
- `Apply`：O(n + k + m)，n 为批次 op 数，k 为当前记录数（索引浅拷贝），m 为变更记录排序（≤ n log n）。
- `Snapshot`：O(k log k + V)，V 为 Value 总字节。
- `Stats`：O(k)。`Clone`：O(k + V)。`Preview`：等价于一次 `Clone` 加一次 `Apply`。
