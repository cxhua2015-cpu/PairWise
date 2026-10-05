# imagecatalog

并发安全的内存型镜像目录，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护两个冗余计数器：`totalValueBytes`（Value 总字节数）与 `nextRevision`（下一个待分配 revision），随每次 Put/Delete 增量更新，避免批次末全表扫描求和。
- 名称合法性（非空、`[a-z0-9-_]`、字节长度上限）在索引查找前完成校验。

### 候选事务（candidate transaction）
- `Apply` 先做整批结构校验（kind、名称、Value 长度、Delete 不携带 Value），不触碰任何状态。
- 校验通过后，在写锁内浅拷贝记录 map 作为候选状态；存储的 `Record` 一旦提交即不可变，因此拷贝廉价且与已提交状态共享无风险。
- 操作按输入顺序应用到候选状态：Put 分配连续 revision，Delete 不分配；Delete 缺失记录返回 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末对候选状态检查，越限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选状态，`generation`、`nextRevision`、索引与计数器全部不变（天然回滚）；成功才一次性提交，非空批次 `generation` 恰好加一。

### 所有权
- 写入：Put 的 `Value` 在入库前拷贝，调用方之后修改入参不影响目录。
- 读出：`Get`/`Snapshot`/`Result.Changed` 中的 `Value` 均为深拷贝，调用方修改返回值不影响内部状态。
- 快照与变更记录按名称排序，结果与内部 map 迭代顺序无关。

### 并发
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁可并行。
- 不可变记录 + 拷贝边界保证读路径无需额外防御。

### 复杂度
- `Apply`：O(k + n)，k 为批内操作数，n 为当前记录数（候选 map 浅拷贝）；校验、应用与容量检查均为 O(k)。
- `Get`：O(1) 均摊（外加一次 Value 拷贝）。
- `Snapshot`：O(n log n)，主要来自按名称排序。
- 空间：O(n + 总 Value 字节数)。
