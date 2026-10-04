# casstore

`casstore` 是一个仅依赖 Go 标准库（Go 1.22+）的并发安全内存条件事务键值存储，面向控制平面元数据场景。完整语义见 `SPEC.md`。

## 索引与存储结构

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位；无二级索引。
- 条目保存 `Key`、`Revision`、`Value`；存储额外维护 `valueBytes`（存活值总字节）、`generation`、`nextRevision` 三个计数器。
- 所有方法通过一把 `sync.Mutex` 串行化，保证并发安全和事务的原子性。

## 比较（Compare）

支持 `Exists`、`NotExists`、`Revision`、`Value` 四类。所有比较与写入先做结构校验（键合法性、Kind 合法、字段组合合法、值长度上限），全部通过后才读取状态；比较在事务开始时的快照上求值，任一不成立即返回 `Succeeded:false`，无错误、无状态变更。

## 事务（Transact）

1. 依次校验全部 Compare，再依次校验全部 Write，最后才进入临界区。
2. 比较全部成立后，在隔离的候选 map（原状态的拷贝）上按输入顺序执行 Put/Delete；Delete 缺失键返回 `ErrNotFound`。
3. 全部写完成后才检查最终容量；任何失败整体回滚，状态、revision、generation 均不变。
4. 只读事务（无写入）成功时不增加 generation；含写入的成功事务 generation 恰好加一。

## Revision 分配

- `nextRevision` 从 1 开始；每个 Put（包括同键重复写、删除后重建）获得当前候选 `nextRevision` 并递增，因此同批写入的 revision 连续。
- 回滚不消耗 revision；`TxnResult.Revision` 为最后一次分配的 revision，未曾 Put 时为 0。

## 容量

- 单值上限 `MaxValueBytes`（结构校验阶段）；键数上限 `MaxKeys` 与存活值总字节上限 `MaxKeys*MaxValueBytes` 在全部写入完成后检查，超限返回 `ErrCapacity` 并回滚。

## 所有权

- 输入 Value 在写入时深拷贝；`Get`/`List`/`Snapshot` 返回的 Value 均为深拷贝，调用方与存储互不影响。

## 分页与快照

- `List(prefix, after, limit)`：空 prefix/after 合法，非空需结构合法，`limit` 为 1..1000；返回键以 prefix 开头且字典序大于 after 的条目，按键升序，可用最后一条键作为 after 稳定翻页。
- `Snapshot` 返回 generation、NextRevision、键数、值字节数及按键排序的条目深拷贝。

## 实际复杂度

- `Transact`：校验 O(C+W)，候选拷贝 O(N)，写入 O(W)，合计 O(N+C+W)，N 为键数。
- `Get` O(1)；`List`/`Snapshot` O(N log N)（排序）；空间 O(N)。
