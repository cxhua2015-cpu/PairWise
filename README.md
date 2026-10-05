# resourceledger162

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
账户主存储为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot`
在读锁内将 map 物化为切片后排序：Top 按数值降序、名称升序，Snapshot 按名称
升序。未维护持久有序索引——账户数受 `MaxAccounts` 上限约束，按需排序更简单
且不会拖慢写路径。

### 候选事务
`Apply` 先做整批结构校验（kind、名称字符集与字节上限），再在**候选副本**
（克隆的 map + 克隆的 revision 计数器）上按输入顺序执行 Add/Set/Delete。
int64 溢出在算术前检测，绝对值上限逐操作执行，账户容量仅在批次末检查。
任何失败直接丢弃候选副本，已提交状态、generation 与 revision 完全不变
（整体回滚）；成功时一次性换入候选副本，非空批次 generation 恰好加一。

### 所有权
所有公开方法由一把 `sync.RWMutex` 保护：写路径（Apply）持写锁，读路径
（Top/Snapshot）持读锁。返回的切片均为新分配的副本，调用方修改不会影响
内部状态；`Account` 为纯值类型，不存在共享引用。

### 复杂度
设 n 为账户数、k 为批次数：
- `Apply`：结构校验 O(k)，候选克隆 O(n)，执行 O(k)，提交 O(1) 换入。
- `Top`：O(n log n) 排序后取前 m 个。
- `Snapshot`：O(n log n) 排序。
- 空间：O(n + k)。
