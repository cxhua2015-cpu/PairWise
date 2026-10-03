# hashring

并发安全的内存加权一致性哈希环（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`。

## 索引结构

`Ring` 内部维护两份状态：节点表 `map[ID]nodeState`（权重与 Value）和有序 token
数组。所有公开方法通过 `sync.RWMutex` 保护：`Apply`/`ApplyBatch` 取写锁，
`Lookup`/`Snapshot` 取读锁，可并发调用且通过 race detector。

## token 构造与碰撞顺序

每个权重为 W 的节点生成 W 个虚拟 token，第 r 个（r 从 0 开始）的哈希输入严格为
`ID + "#" + 十进制r`（无前导零）。默认 Hasher 取 SHA-256 的前 8 字节大端 uint64；
自定义 Hasher 同时用于 key 与 token。token 按 `(Hash升序, NodeID字节序升序,
Replica升序)` 排序，哈希碰撞不丢 token，碰撞块内顺序完全确定。

## Lookup

对 `hash(key)` 二分查找第一个 `Hash >= hash(key)` 的 token，越界则回绕到下标 0，
顺时针扫描并跳过已返回过的 NodeID，返回 `min(count, 节点数)` 个 Owner，顺序即选择
顺序。key 为空或 count 不在 `1..MaxNodes` 返回 `ErrInvalidLookup`，空环返回
`ErrEmpty`。

## 批量事务

`Apply` 等价于单元素 `ApplyBatch`；空批返回当前 generation。先对全部变更按输入
顺序做结构校验（类型、字段留空规则、ID 合法性、Weight、单 Value 大小），再在候选
副本上按序执行语义操作（Add 查重 `ErrDuplicate`，Remove/Update 查存在
`ErrNotFound`，批内允许 Remove 后 Add 同一 ID）。只在最终状态检查 MaxNodes、
Weight 之和对应的 MaxTokens、Value 总字节预算，任一失败整体回滚、零副作用；非空
成功批 generation 只加一。

## 容量

`MaxNodes` 1..10000，`MaxTokens` 1..1,000,000，`MaxValueBytes` 1..64 MiB，非法
Options 返回 `ErrInvalidOptions`。节点 ID 为 1..64 字节（ASCII 字母、数字、`.`、
`_`、`-`），Weight 1..128，单 Value ≤ 1 MiB。

## 所有权

输入 Node 的 Value 在提交时深拷贝；`Snapshot` 的 Nodes/Tokens、`Lookup` 的 Owner
Value 均为独立副本，调用方修改任何返回值或输入缓冲区都不会影响环内状态，反之
亦然。

## 实际复杂度

设 N 为节点数、T 为 token 总数（等于 Weight 之和）、B 为变更数：

- `ApplyBatch`：校验与候选复制 O(N + B)，token 重建 O(T log T)，每次成功批次全量
  重建有序数组。
- `Lookup`：二分定位 O(log T)，扫描最坏 O(T)，结果去重 O(count)。
- `Snapshot`：O(N log N + T)。
- 内存：O(N + T + Value 总字节)。
