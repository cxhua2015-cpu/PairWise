# jsonpatch

并发安全的内存 JSON Patch（RFC 6902 风格）文档存储，Go 1.22+，仅依赖标准库。公开 API 位于 `jsondoc` 包。

## 内部 JSON 表示

文档解码为 Go 原生值树：`map[string]any`（对象）、`[]any`（数组）、`string`、`json.Number`、`bool`、`nil`。解码使用 `json.Decoder.UseNumber`，数字保留原始词素（lexeme），因此 `1.50e2`、大整数等不会被 float64 精度吞掉。对象成员名不是节点。

## JSON Pointer 解析

- 空字符串指向文档根；非空必须以 `/` 开头，按 `/` 切分 token。
- 转义仅支持 `~1` → `/`、`~0` → `~`，其余 `~` 序列报 `ErrInvalidPointer`；对象 token 允许为空字符串。
- 数组下标必须是 `0` 或不带前导零的十进制正整数；前导零、符号、溢出、`-` 均为 `ErrInvalidIndex`，只有 `add` 的最后一个 token 允许用 `-` 表示追加。
- 访问下标必须小于长度（越界 `ErrNotFound`）；`add` 的插入下标可以等于长度。
- 对标量继续深入报 `ErrTypeMismatch`，对象成员缺失报 `ErrNotFound`。

## 六类操作

- `add`：根位置整体替换；对象中创建或覆盖成员；数组中在下标前插入或用 `-` 追加。
- `remove`：删除已存在的对象成员或数组元素；删除根报 `ErrRootRemoval`。
- `replace`：替换已存在的值，允许替换根。
- `copy`：深拷贝 `From` 处值，再按 add 语义写入 `Path`。
- `move`：`From == Path` 时为成功 no-op；禁止移动到自身严格后代（`ErrMoveIntoChild`，含把根移到非根位置）；否则先删除 `From`，再对删除后的文档按 add 语义写入 `Path`。把子节点移到根是允许的。
- `test`：按 JSON 语义比较，失败报 `ErrTestFailed`。

## 批量原子性与校验顺序

`Apply` 的处理顺序固定为：

1. 对全部操作做结构校验（操作名、`Value`/`From` 存在性与 JSON 合法性、Pointer 合法性），错误包装为带零基下标的 `*OpError`。
2. 比较 `expectedRevision` 与当前 revision，不等则返回未包装的 `ErrRevisionConflict`。
3. 在文档的隔离深拷贝上按输入顺序执行操作，后面的操作能看到前面的结果；任一失败返回 `*OpError` 并整体回滚。
4. 所有操作执行完后才检查节点预算，允许批次内临时超限；最终超限返回 `ErrCapacity` 并回滚。

任何失败都返回零值 `Result`，文档与 revision 不变。

## Revision

初始 revision 为 1。成功批次中只要含至少一个 `add`/`remove`/`replace`/`move`/`copy`（包括 move 的 no-op），revision 恰好推进一次，即使最终 JSON 语义不变；空批次或纯 `test` 批次不改变 revision。

## 节点计数

每个对象、数组、字符串、数字、布尔、null 各计 1 个节点（对象成员名不计）。初始文档和每批最终状态都不得超过 `Options.MaxNodes`（`<= 0` 报 `ErrInvalidOptions`）。

## 数值比较

`test` 中数字按精确数学值比较：优先用 `math/big.Rat` 解析词素做精确比较（`1`、`1.0`、`1e0` 相等，且大整数不会因 float64 舍入误判）；无法精确解析时退化为 float64 比较。对象成员顺序与数字词素差异不影响相等性。

## Payload 所有权

- 输入：`New` 的初始字节与 `Operation.Value` 在返回前即完成解码拷贝，调用方随后修改原切片不影响存储。
- 输出：`Result.Document` 与 `Snapshot.Document` 每次都是重新序列化的紧凑 JSON（对象键按字典序排序，数字词素保留），与内部状态及其它返回值互不共享内存。

## 并发

所有公开方法通过一把 `sync.Mutex` 串行化；结构校验在锁外进行（纯函数），revision 比较、执行与提交在临界区内原子完成，等价于基于期望 revision 的 CAS。

## 复杂度

设文档节点数为 N，批次操作数为 M，单条 Payload 大小为 V：

- 时间：结构校验 O(M·(指针长度 + V))；每批一次深拷贝 O(N)；每个操作的路径定位 O(路径深度)，数组插入/删除 O(数组长度)；最终节点计数 O(N)；结果序列化 O(N)。整体 O(N·(M 的数组搬运) + N)，典型为 O(N + M·d)。
- 空间：每批一份工作深拷贝 O(N)，结果文档 O(N)；`Snapshot` 序列化 O(N)。失败批次不留下任何中间状态。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
