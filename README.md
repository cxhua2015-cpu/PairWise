# resourcecatalog196

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录；`Snapshot` 与 `Result.Changed` 在返回前对名称排序，保证确定性输出。
- 单把 `sync.Mutex` 保护全部内部状态（records、totalValues、generation、nextRev），所有公开方法并发安全。

### 候选事务（candidate transaction）
`Apply` 分三个阶段：
1. **结构校验**：在持有锁、读取任何状态之前，校验全部 Op 的 kind、名称字符集/长度、Value 长度；任何失败返回 `ErrInvalidInput`，不产生副作用。
2. **候选执行**：在锁内把当前 records 浅拷贝到候选 map，按输入顺序执行 Put/Delete。Put 分配连续 revision（从 `nextRev` 起），Delete 不分配；Delete 缺失记录返回 `ErrNotFound`。
3. **提交或回滚**：批次末检查记录数与 Value 总字节容量，超限返回 `ErrCapacity`。失败时直接丢弃候选 map，内部状态、generation、revision 完全不变（回滚零成本）；成功时整体替换 records 并将 generation 加一（空批次不加）。

### 所有权
- 写入时深拷贝 `Op.Value`，调用方之后修改入参切片不影响目录。
- `Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，返回切片与内部状态完全隔离。

### 复杂度
- `Apply`：校验 O(B)，执行 O(B + N)（N 为当前记录数，候选拷贝），排序 Changed O(K log K)。
- `Get`：O(1) 平均 + O(V) 拷贝。
- `Snapshot`：O(N log N) 排序 + O(总字节) 深拷贝。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
