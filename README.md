# servicecatalog

并发安全的内存型服务目录，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，以名称作为唯一键，Put/Delete/Get 均为 O(1) 均摊查找。
- `Snapshot` 与 `Result.Changed` 在返回前对名称排序（`sort.Strings`），保证输出确定性。

**候选事务（candidate transaction）**
- `Apply` 分三个阶段：先做整批结构校验（kind、名称字符集与长度、Value 长度、Delete 不携带 Value），不触碰任何状态；然后在当前记录的候选副本上按输入顺序执行 Put/Delete（Put 分配连续 revision，Delete 不分配且要求目标存在）；最后才在批次末检查记录数与 Value 总字节容量。
- 任一步失败直接丢弃候选副本，正式状态、generation、revision 完全不变，天然回滚；成功时整体换入候选副本，非空批次 generation 只加一。

**所有权**
- 存入的 Value 在写入时深拷贝，调用方之后修改入参不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。

**并发**
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可安全并发调用（`go test -race` 验证）。

**复杂度**
- `Apply`：O(n + m)，n 为批内 op 数，m 为当前记录数（候选复制与容量求和）。
- `Get`：O(1) 均摊；`Snapshot`：O(m log m)（排序）。
