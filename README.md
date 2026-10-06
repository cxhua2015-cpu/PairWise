# metacatalog291

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete，失败整体回滚；Get/Snapshot 深拷贝返回值，Snapshot 按名称排序。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot` 与逻辑时钟。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套
  结构语义（名称字符集/长度、kind、Put 非空值、Delete 必须 nil 值），不读取状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁下一次取齐 generation、nextRevision、
  记录数与 Value 总字节，与并发事务的某一原子时刻一致。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 复制全部记录 Value 并保留逻辑时钟，
  克隆体与原实例不共享任何可变内存。

## 索引

记录存放在以名称为键的哈希表中，点查 `Get` 为 O(1)。有序视图（`Snapshot`、
`Result.Changed`）按需对键排序物化，不维护额外有序索引。

## 候选事务

`Apply` 先完整结构校验（不读状态），再在写锁内把记录表复制为候选副本，
按输入顺序应用 Put/Delete：Put 从本地候选 revision 计数器分配连续 revision，
Delete 不分配。记录数上限与 Value 总字节上限只对批次末的候选状态检查。
任一失败直接丢弃候选，原表、generation 与 revision 完全不变；成功才一次性提交，
非空批次 generation 恰好加一，空批次不改变任何时钟。

## 所有权

跨边界一律复制：Put 存入前拷贝 Value，`Get`/`Snapshot`/`Result.Changed`/`Clone`
返回前再次拷贝，调用方对返回切片的任何修改都不会影响目录内部状态，反之亦然。

## 复杂度

- `Get`：O(1)；`ValidateBatch`：O(批次总字节)。
- `Apply`：O(R + B)，R 为当前记录数（候选复制），B 为批次大小；排序变更集 O(K log K)。
- `Snapshot`/`Stats`/`Clone`：O(R)，其中 `Snapshot`/`Changed` 另含 O(R log R) 排序。
- 并发：读写锁分离，读路径（Get/Snapshot/Stats/Clone）互不阻塞，写事务串行提交。
