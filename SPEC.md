# hashring contract

## 公共边界

- `MaxNodes` 1..10000，`MaxTokens` 1..1,000,000，`MaxValueBytes` 1..64 MiB。
- 合法 ID 为 1..64 字节，仅含 ASCII 字母、数字、点、下划线、连字符；Weight 为 1..128；单个 Value 最多 1 MiB。
- `Hasher` 为 nil 时使用 SHA-256 输入的前 8 字节大端 uint64。非 nil Hasher 用于全部 key 与 token 哈希，调用时传入的字节不得被后续修改。
- 第 r 个虚拟 token（r 从 0 开始）的输入严格为 `ID + "#" + 十进制r`，无前导零。

## 批量事务

- `Apply` 等价于单元素 `ApplyBatch`；空批返回当前 generation。
- 先按输入顺序完成全部结构校验，再在候选副本按输入顺序执行语义操作。结构错误优先于重复或缺失错误。
- Add 要求 `Change.ID` 为空；Remove 要求 `Change.Node.ID` 为空；Update 要求 `Change.ID` 合法且 `Change.Node.ID` 为空，并用 Node 的 Weight/Value 替换现值。
- Add 重复返回 `ErrDuplicate`；Remove/Update 缺失返回 `ErrNotFound`。批内可 Remove 后 Add 同一 ID。
- 最终检查 MaxNodes、所有 Weight 之和对应的 MaxTokens、Value 总字节。失败零副作用；非空成功批 generation 加一。

## token 与 Lookup

- token 按 `(Hash升序, NodeID字节序升序, Replica升序)` 排序；哈希碰撞不丢 token。
- `Lookup(key,count)` 要求 key 非空，count 为 1..MaxNodes。空环返回 `ErrEmpty`。
- 从第一个 `Hash >= hash(key)` 的 token 开始顺时针并回绕，跳过已经返回过的 NodeID，返回 `min(count,节点数)` 个 Owner。
- Owner 顺序即选择顺序，Value 深拷贝，Generation 为当前值。

## Snapshot 与并发

- Nodes 按 ID 升序；Tokens 使用环顺序。所有 Value 与输入及其他返回值隔离。
- 所有公开方法并发安全并通过 race detector。
- 错误可包装 sentinel，但 `errors.Is` 必须成立。
