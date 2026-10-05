# secretcatalog

并发安全的内存型密钥元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]entry` 为主索引，键即记录名，`entry` 保存
深拷贝后的 Value 与 Revision；另维护 `totalBytes` 计数避免每次容量检查都
全表求和。单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot`
取读锁，因此所有公开方法可并发调用。

**候选事务**：`Apply` 分三个阶段——(1) 完整结构校验（kind、名字字符集与
长度、Value 长度、Delete 不带 Value），不读取任何状态；(2) 在克隆出的候选
map 上按输入顺序执行 Put/Delete，Put 分配连续 revision 并维护总字节数，
Delete 不分配 revision、缺失即 `ErrNotFound`；(3) 仅在批次末检查最终记录数
与 Value 总字节容量。任何阶段失败都直接丢弃候选状态，committed map、
generation、revision 全部保持不变；成功时整体换入候选 map，非空批次
generation 恰好加一。

**所有权**：Put 的 Value 在提交前深拷贝，调用方之后修改入参不影响目录；
`Get`/`Snapshot`/`Result.Changed` 返回的 Value 同样是深拷贝，调用方修改
返回值不会污染内部状态。`Snapshot` 与 `Changed` 均按名称排序，返回切片与
内部状态完全隔离。

**复杂度**：`Apply` 为 O(n + m log m)，n 为批次内 op 数，m 为变更的不同
记录数（Changed 排序），候选克隆为 O(R)，R 为当前记录数；`Get` 为 O(1)
均摊（含 O(v) 拷贝，v 为 Value 长度）；`Snapshot` 为 O(R log R)（排序）。
空间 O(R + 总 Value 字节数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
