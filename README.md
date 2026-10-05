# routecatalog

并发安全的内存型路由目录，语义见 `SPEC.md`。

## 设计说明

- **索引**：记录存放在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；`Get`/`Snapshot` 使用 `sync.RWMutex` 读锁，`Apply` 使用写锁，所有公开方法并发安全。
- **候选事务**：`Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；随后在写锁内把当前 map 复制为候选副本，按输入顺序在副本上执行 Put/Delete。Put 从 `nextRevision` 起分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查；任一失败直接丢弃候选副本，状态、generation 与 revision 全部回滚。提交时原子替换 map，非空成功批次 generation 只加一。
- **所有权**：Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。`Snapshot` 与 `Changed` 按名称排序。
- **复杂度**：`Apply` 为 O(n + m)，n 为当前记录数（候选复制），m 为批次数；`Get` 为 O(1)；`Snapshot` 为 O(n log n)（排序）。额外维护 `totalValue` 计数器使容量检查为 O(1)。
