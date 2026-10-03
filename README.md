# netpolicy

这是并发网络策略表的实现。完整合同见 `SPEC.md`。

## 设计说明

### 规则索引

规则以 ID 为主键存放在 `map[string]*storedRule` 中，增删按 ID O(1) 定位。匹配采用全表线性扫描加逐条比较，未维护前缀树/线段树等专用索引——规则上限为 10000，线性扫描在边缘防火墙规模下足够快，且保证裁决逻辑与合同完全一致、无索引一致性问题。表级并发由一把 `sync.RWMutex` 保护：写操作（`Apply`/`ApplyBatch`）持写锁，读操作（`Match`/`Snapshot`）持读锁。

### 地址与前缀规范化

- 加入规则时，前缀先经 `netip.ParsePrefix` 解析，再将 IPv4-mapped IPv6 地址（`::ffff:a.b.c.d`，bits >= 96）折算为对应 IPv4 前缀（位数减 96），最后 `Masked` 清除主机位；规范字符串（如 `192.0.2.0/24`、`2001:db8::/32`）被缓存并用于 `Decision`/`Snapshot` 输出。
- `Match` 的查询地址同样 `Unmap`，因此 mapped 查询只会命中 IPv4 规则，不会误配原生 IPv6 规则；带 zone 的地址被拒绝。

### 匹配优先级

候选规则须前缀包含地址，且协议为 Any 或与请求一致（具体协议还要求端口落在闭区间内）。候选依次按以下键择优：前缀位数更多 → 具体协议优于 Any → 端口区间宽度更小（Any 视为 65536）→ Priority 更高 → Deny 优于 Allow → ID 字节序更小。比较键覆盖全部字段，裁决完全稳定。

### 批量事务

`ApplyBatch` 分两阶段：先按输入顺序对全部 change 做结构校验（ID、前缀、协议、端口、优先级、动作、Value 大小），再在候选副本（原 map 的浅拷贝，规则对象不可变共享）上按输入顺序执行 Add/Delete 语义操作。同一批支持 Delete 后重 Add 同一 ID。只有全部成功后才对最终状态检查 `MaxRules` 与 `MaxValueBytes`；任一失败直接丢弃候选，原表、generation 与容量计数不变。非空成功批次 generation 恰好加一；空批成功且不改变状态。

### 容量计数

`UsedValueBytes` 只统计当前存活规则的 Value 字节数：Add 累加、Delete 扣减，随候选状态一起计算，失败批次不影响计数。单条 Value 上限 1 MiB，表级预算上限 64 MiB。

### 所有权

输入 `Rule.Value` 在 Add 时深拷贝；`Decision.Value` 与 `Snapshot` 中的 Value 在返回前深拷贝。调用方修改输入或返回值均不会影响表内状态，多次返回值之间也互不共享内存。

### 复杂度

设 n 为规则数、b 为批次大小：

- `ApplyBatch`：时间 O(n + b)，空间 O(n + b)（候选 map 浅拷贝与解析缓存）。
- `Match`：时间 O(n)，空间 O(1)（不计返回值拷贝）。
- `Snapshot`：时间 O(n log n)（排序），空间 O(n)。
- 总内存：O(n + 存活 Value 字节数)。
