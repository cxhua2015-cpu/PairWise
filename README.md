# metacatalog426

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；失败回滚全部状态、generation 与 revision。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot`，错误值与公开类型。
- `validation.go` — 无副作用批次预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value）。
- `stats.go` — 线性一致的状态统计 `Stats`（generation、nextRevision、记录数、Value 总字节）。
- `clone.go` — 保留逻辑时钟（generation/nextRevision）且所有权完全隔离的深拷贝 `Clone`。
- `preview.go` — 事务预演 `Preview`：在一次线性化快照上复用完整 `Apply` 语义，返回候选 `Result`/`Snapshot`/`Stats`，不改变原对象状态与逻辑时钟；错误及优先级与同状态 `Apply` 一致，失败时全部返回零值。

## 索引

记录存储于 `map[string][]byte`（名称 → Value），另以 `map[string]uint64` 保存各名称的 revision。`Get` 为 O(1) 哈希查找；`Snapshot`/`Result.Changed` 在读取时按名称排序输出，不维护额外有序结构。

## 候选事务

`Apply` 先在完整结构校验通过后，于内存中构建候选 map（写时复制语义：仅复制 map 头与键，Value 仅在 Put 时拷贝），按顺序执行全部操作，批次末统一检查记录数与 Value 总字节容量；任一失败直接丢弃候选，原状态、generation、revision 不变。`Preview` 通过 `Clone` 获得候选 Store 后在其上执行同一 `Apply`，天然保证语义与错误优先级一致。

## 所有权

所有跨边界的数据（Apply 的输入 Value、Get/Snapshot/Result/Clone 的输出 Value）都做深拷贝，返回切片与内部状态及候选状态完全隔离；调用方修改返回值不影响 Store，反之亦然。

## 并发与复杂度

单个 `sync.Mutex` 保护全部状态，所有公开方法可并发调用且各自线性化。设批次含 k 个操作、当前 n 条记录：

- `ValidateBatch`：O(k·L)，L 为名称长度；不读取状态。
- `Apply`：O(n + k + c log c)，c 为变更名称数（排序）；回滚零额外成本。
- `Get`：O(1)（外加 Value 拷贝）；`Snapshot`：O(n log n)；`Stats`：O(n)。
- `Clone`：O(n + 总字节数)；`Preview` = Clone + Apply + Snapshot + Stats。
