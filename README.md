# metacatalog361

并发安全的内存型“元数据目录 361”（Go 1.22+，仅标准库）。实现见 `metacatalog361/servicecatalog.go`，语义以 `SPEC.md` 与契约测试为准。

## 索引

- 主索引为 `map[string]entry`，按名称 O(1) 定位记录；`entry` 保存深拷贝后的 Value 与分配的 revision。
- 另维护 `totalBytes`（Value 总字节）与单调递增的 `revision` / `generation` 计数器，容量判定无需遍历。
- `Snapshot` 与 `Result.Changed` 在返回前对名称排序（`sort.Strings`），保证输出确定性。

## 候选事务

- `Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不带 Value），任何失败直接返回 `ErrInvalidInput`，不读取任何状态。
- 校验通过后，在记录集合的拷贝（候选事务）上按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失键返回 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`；批次中间态允许瞬时超限。
- 任一失败直接丢弃候选，原始 map、generation、revision 完全不变（天然回滚）；成功时整体换入候选，非空批次 generation 只加一，空批次不变。

## 所有权

- Put 的 Value 在写入前深拷贝，调用方之后修改入参不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。

## 并发

- 全部公开方法由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Get`/`Snapshot` 取读锁，可安全并发调用。

## 复杂度

- `Apply`：O(B + N)，B 为批次数（校验与执行），N 为当前记录数（候选拷贝）；排序 Changed 为 O(C log C)。
- `Get`：O(1) 查询 + O(V) 拷贝；`Snapshot`：O(N log N)。
- 空间：O(N + 总 Value 字节)。
