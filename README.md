# hashring

并发安全的内存加权一致性哈希环（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`。

## 索引结构

`Ring` 内部维护两份状态，由一把 `sync.RWMutex` 保护：

- `nodes map[string]*nodeState`：节点 ID 到 `{weight, value}` 的映射，是语义操作的权威状态。
- `tokens []token`：按 `(Hash 升序, NodeID 字节序升序, Replica 升序)` 排序的虚拟 token 数组，供 Lookup 二分查找。

所有写操作（`Apply`/`ApplyBatch`）持写锁；`Lookup`/`Snapshot` 持读锁，可并发执行。

## token 构造

每个节点按 Weight 生成 `Weight` 个虚拟 token。第 r 个（r 从 0 开始）token 的哈希输入严格为 `ID + "#" + 十进制r`（无前导零）。默认 Hasher 为 SHA-256 的前 8 字节大端 uint64；自定义 Hasher 同时用于 key 与 token 哈希，传入的字节切片在调用后不会被修改。

## 碰撞顺序

哈希碰撞不丢 token：排序键为 `(Hash, NodeID, Replica)` 三元组，全部相同哈希的 token 按节点 ID 字节序、再按副本序号确定性地排列，因此相同状态在任何进程中都产生相同的环。

## 批量事务

`Apply` 等价于单元素 `ApplyBatch`；空批返回当前 generation。非空批的处理流程：

1. 按输入顺序对全部 Change 做结构校验（类型、字段组合、ID 格式、Weight 范围、单 Value ≤ 1 MiB），任一失败即返回，结构错误优先于语义错误。
2. 在候选副本（节点表的浅拷贝，Value 深拷贝）上按输入顺序执行语义操作：Add 重复报 `ErrDuplicate`，Remove/Update 缺失报 `ErrNotFound`；批内允许 Remove 后重新 Add 同一 ID，Update 用新 Weight/Value 整体替换。
3. 仅在最终状态检查容量：节点数 ≤ `MaxNodes`、Weight 总和 ≤ `MaxTokens`、Value 总字节 ≤ `MaxValueBytes`，违反报 `ErrCapacity`。

任一步失败零副作用（原状态完全不变）；成功的非空批重建 token 数组并将 generation 加一（且只加一）。

## 容量

- `MaxNodes` 1..10000，`MaxTokens` 1..1,000,000，`MaxValueBytes` 1..64 MiB，在 `New` 时校验。
- 节点 ID 为 1..64 字节，仅含 ASCII 字母、数字、`.`、`_`、`-`；Weight 为 1..128；单个 Value ≤ 1 MiB。

## 所有权隔离

- 输入：`ApplyBatch` 中的 `Node.Value` 在提交时深拷贝，调用方之后修改不影响环。
- 输出：`Snapshot` 的 `Nodes[].Value`、`Lookup` 返回的 `Owner.Value` 均为深拷贝，调用方修改不会污染环内状态；多次 `Snapshot` 之间也互不相干。
- `Snapshot.Nodes` 按 ID 升序，`Snapshot.Tokens` 为环顺序。

## 复杂度

设 N 为节点数，T 为 token 总数（Weight 之和），B 为批大小：

- `ApplyBatch`：结构校验 O(B)；语义操作 O(B)；容量合计 O(N)；成功时重建并排序 token O(T log T)。
- `Lookup`：二分定位 O(log T)，顺时针扫描去重，每次 O(T) 上界、通常远小；空间 O(min(count, N))。
- `Snapshot`：O(N log N + T)。
- 内存：O(N + T + Value 总字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
