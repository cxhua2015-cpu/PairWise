# modelcatalog

并发安全的内存型模型目录，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 设计说明

- **索引**：`Store` 内部使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；
  `generation`（成功非空批次计数）与 `nextRevision`（下一个待分配 revision）随索引一起
  由一把 `sync.Mutex` 保护，所有公开方法（`Apply`/`Get`/`Snapshot`）均可并发调用。
- **候选事务**：`Apply` 分两阶段。第一阶段对整个批次做完整结构校验（kind、名称字符集
  与长度、Delete 不得携带 Value、单值字节上限），不读取任何状态；随后在克隆出的候选
  索引上按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配。记录数与
  Value 总字节容量只在批次末检查。任一步失败直接丢弃候选，状态、generation 和
  revision 全部不变；成功则整体提交，generation 只增加一次。
- **所有权**：写入时深拷贝输入 Value，读取（`Get`/`Snapshot`/`Result.Changed`）时深拷贝
  输出 Value，调用方对返回切片的任何修改都不会影响内部状态，反之亦然。
- **复杂度**：设批次含 B 个操作、当前记录数为 N、单值最大 L 字节。`Apply` 为
  O(N + B·L)（克隆候选索引 + 应用操作 + 末尾容量汇总），`Get` 为 O(L)，
  `Snapshot` 为 O(N·log N + N·L)（按名称排序 + 深拷贝）。
