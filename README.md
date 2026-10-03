# netpolicy

并发安全的内存网络策略表（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`，公开 API 见 `netpolicy/netpolicy.go`。

## 规则索引

规则以 `map[string]*entry` 按 ID 存储，每个 entry 缓存解析并规范化后的 `netip.Prefix`。匹配与快照为全表线性扫描（规则数 ≤ 10000，边缘场景可接受），换取实现简单、无锁层级、无索引不一致风险。

## 地址与前缀规范化

- 入库前缀：`netip.ParsePrefix` 解析 → `Unmap()` 把 IPv4-mapped IPv6 折成 IPv4（位数减 96）→ `Masked()` 清主机位。例如 `::ffff:192.0.2.129/120` 规范为 `192.0.2.0/24`，`10.9.8.7/8` 规范为 `10.0.0.0/8`。
- 匹配地址：`netip.ParseAddr` 解析（拒绝 zone），同样 `Unmap()`，因此 `::ffff:192.0.2.4` 按 IPv4 匹配。
- Snapshot 与 Decision 一律返回规范字符串。

## 匹配优先级

候选 = 前缀包含地址 且（协议为 Any，或协议相等且端口落在闭区间内）。按以下键依次择优，完全确定：

1. 前缀位数更多（最长前缀优先）
2. 具体协议（TCP/UDP）优于 Any
3. 端口区间宽度更小（Any 视为 65536）
4. Priority 更高
5. Deny 优于 Allow
6. ID 字节序更小

## 批量事务

`ApplyBatch` 两阶段执行：

1. **结构校验**：按输入顺序校验所有 change 的字段（ID → Prefix → Protocol → Port → Priority → Action → Value 大小），任何错误立即返回，结构错误优先于重复/缺失错误。
2. **语义执行**：在候选副本上按输入顺序执行 Add/Delete（支持同批 Delete 后重 Add 同 ID），再对**最终状态**检查 `MaxRules` 与 `MaxValueBytes`。

任何失败整体回滚：规则、generation、容量计数与输入数据均不变。非空成功批次 generation 恰好 +1；空批次成功返回当前 generation。`Apply` 等价于单元素 `ApplyBatch`。

## 容量计数

`UsedValueBytes` 只统计当前规则的 Value 字节总和，随 Add/Delete 增减；只在批次成功结束时与 `MaxValueBytes`（以及规则数与 `MaxRules`）比较，中间状态允许超限。

## 所有权

- 入库时 `bytes.Clone` 拷贝输入 Value，调用方之后修改输入切片不影响表内状态。
- `Match`/`Snapshot` 返回的 Value 均为深拷贝，调用方修改返回值不影响表，也不影响后续调用结果。

## 并发

所有公开方法通过 `sync.RWMutex` 保护：写路径（Apply/ApplyBatch）持写锁，Match/Snapshot 持读锁可并行。`go test -race ./...` 通过。

## 复杂度

设 n = 规则数，b = 单批 change 数，v = Value 总字节数。

- `ApplyBatch`：时间 O(n + b)（克隆 map 指针 + 逐条执行），空间 O(n)（候选副本为浅拷贝，仅 Value 深拷贝新增部分）。
- `Match`：时间 O(n)，空间 O(1)（不计返回值拷贝）。
- `Snapshot`：时间 O(n log n)（排序），空间 O(n + v)。
- 表总空间 O(n + v)。

## 运行

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
