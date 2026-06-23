# go-pcre2-lite

[![CI](https://github.com/VillanCh/go-pcre2-lite/actions/workflows/ci.yml/badge.svg)](https://github.com/VillanCh/go-pcre2-lite/actions/workflows/ci.yml)

[English](README.md) | **简体中文**

一个高性能、可嵌入的 Go 正则表达式库：底层是一份经过裁剪的 **PCRE2 8 位解释器**，
由随包 vendoring 的 C 源码通过 cgo 编译而成（**JIT 永久禁用**），并作为
`github.com/dlclark/regexp2` 的**直接替换（drop-in）**对外提供。

- **drop-in 兼容 `regexp2`** —— 通常只需改一行 import 路径。
- **更快、更省分配** —— 在所有基准上都快于 `dlclark/regexp2`（最高约 9.6 倍）、
  布尔匹配零分配，批量 `FindAll` 在海量小匹配场景下甚至超过 Go 自带的 `regexp`
  （见 [性能](#性能)）。
- **默认 ReDoS 安全** —— 由 PCRE2 的 match/depth 限制兜底；灾难性模式会返回错误而
  不是卡死。
- **特性丰富** —— 捕获组 / 命名组、前后向断言、反向引用、原子组、占有量词、递归、
  `\K`、子程序调用、Unicode 属性。
- **SQLite 式嵌入** —— 无外部库、无 `pkg-config`、无 CMake、无动态链接，唯一要求是
  `CGO_ENABLED=1`。

## 安装

```bash
go get github.com/VillanCh/go-pcre2-lite
```

需要 `CGO_ENABLED=1` 和一套 C 工具链（PCRE2 的 C 源码随包 vendoring 并一起编译，
代码中从不引用 JIT）。

## 使用

### 作为 `regexp2` 的 drop-in 替换（面向 rune，推荐）

```go
import regexp2 "github.com/VillanCh/go-pcre2-lite/regexp2"

re := regexp2.MustCompile(`(?<area>\d{3})-(?<num>\d{4})`, 0)

// 布尔匹配（热路径零分配）。
ok, _ := re.MatchString("call 555-1234")

// 首个匹配 + 命名 / 编号分组。
m, _ := re.FindStringMatch("call 555-1234")
if m != nil {
    fmt.Println(m.GroupByName("area").String()) // 555
    fmt.Println(m.GroupByNumber(2).String())    // 1234
    fmt.Println(m.Index, m.Length)              // rune 下标 / 长度
}

// 遍历所有匹配。
for m, _ := re.FindStringMatch("555-1234 / 777-0000"); m != nil; m, _ = re.FindNextMatch(m) {
    fmt.Println(m.String())
}

// 替换（$1 / ${name} 模板）与 ReplaceFunc。
out, _ := re.Replace("555-1234", "${area}.${num}", -1, -1) // 555.1234
out, _ = re.ReplaceFunc("555-1234", func(m regexp2.Match) string {
    return strings.ReplaceAll(m.String(), "-", " ")
}, -1, -1)

// 转义 / 反转义字面文本。
lit := regexp2.Escape("a.b*c")

// 防御对抗性模式（在 regexp2 API 之外的扩展）。
_ = re.SetMatchLimits(100000, 100000)
```

`Index`/`Length` 是 rune 下标，与 `regexp2` 完全一致。无需 `Close()`（finalizer 会回收
C 内存），与 `regexp2` 的使用习惯一致。编译后的 `*Regexp` 可被多个 goroutine 并发匹配。

### 低层字节 API（追求极致性能）

```go
import pcre2 "github.com/VillanCh/go-pcre2-lite"

re := pcre2.MustCompile(`\w+@\w+\.\w+`, pcre2.CompileOptions{UTF: true, UCP: true})
defer re.Close() // 显式释放；finalizer 仅作兜底

ok, _ := re.Match([]byte("a@b.com"))           // 零分配
m, _ := re.Find([]byte("a@b.com"), 0)          // m.Groups[i] 为字节区间
all, _ := re.FindAll([]byte("a@b.com x@y.io"), -1)
```

低层 API 中所有偏移都是 **UTF-8 字节偏移**（兼容层则是 rune 下标）。需要最低分配和最快
批量 `FindAll` 时请用这一层。

## 从 `github.com/dlclark/regexp2` 迁移

对绝大多数代码而言，迁移就是改一行 import：

```go
// 改之前
import "github.com/dlclark/regexp2"
// 改之后
import regexp2 "github.com/VillanCh/go-pcre2-lite/regexp2"
```

对外暴露的类型、方法、`RegexOptions` 常量、基于 rune 的 `Index`/`Length`、
`Replace`/`ReplaceFunc`、`Escape`/`Unescape` 都与 `regexp2` 对齐。整段匹配结果在
PCRE2 官方 1585 条输入语料上达到 **100%** 一致。需要留意的行为差异：

| 方面 | `dlclark/regexp2`（.NET） | `go-pcre2-lite` |
|---|---|---|
| ReDoS / 超时 | 默认 `MatchTimeout` 为永不超时，可能卡死 | 由 match/depth 限制兜底，返回 `ErrMatchLimit` 而非卡死 |
| `MatchTimeout` 字段 | 强制墙钟超时中断 | 仅为 API 兼容保留，**不**强制生效 —— 请用 `SetMatchLimits` |
| 命名 + 未命名组**混用时的编号** | 先未命名、再命名 | 严格从左到右 |
| 重复组的 `Group.Captures` | 完整捕获历史 | 仅保留最后一次捕获（`.String()` 结果相同） |
| `RightToLeft` | 真正从右到左扫描 | 接受该选项，但引擎始终从左到右扫描 |

只有命名 / 未命名组**混用时的编号**会有差异；按名访问（`GroupByName`）始终一致。
完整细节与逐项差异测试见 [MIGRATION.md](MIGRATION.md)。

## 不支持 / 有差异的语法

移植模式时需要重点检查以下构造。它们分三类，其中**静默差异**这一类最危险。

### 编译期直接拒绝（安全 —— 会立刻报错）

| 构造 | 示例 | 说明 |
|---|---|---|
| .NET 平衡组 | `(?<open>\()[^()]*(?<-open>\))` | .NET 独有的栈特性；PCRE2 无对应实现 |
| 长度依赖反向引用的后向断言 | `(?<=a(.\2)b(\1))` | 编译期无法确定长度上界 |
| 长名 `\p{...}` 类别 | `\p{Number}`、`\p{IsGreek}` | 请用短别名 `\p{N}`、`\p{Greek}`（`dlclark` 同样拒绝长名） |

### 通过兼容层接受（`regexp2` 包）

`regexp2` 这个 drop-in 包会改写少量 .NET/RE2 容忍、但裸 PCRE2 拒绝的构造，使常见场景"开箱即用"：

| 构造 | 示例 | 处理方式 |
|---|---|---|
| 变长后向断言 | `(?<=a+)b`、`(?<="text":\s*")` | PCRE2 10.47 原生支持有界变长后向断言；后向断言内的无界量词被收紧为 `{n,512}` 以便编译与匹配（超过 512 次重复不匹配） |
| 字符类中集合简写紧邻 `-` | `[\d\w-_]`、`[a-\w]` | 将 `-` 视为字面量（与 .NET/RE2 一致），避免 "invalid range in character class" |

### 静默差异（能编译，但行为不同 —— 务必审计）

| 构造 | 示例 | `dlclark`（.NET） | `go-pcre2-lite`（PCRE2） |
|---|---|---|---|
| .NET 字符类减法 | `[a-z-[aeiou]]` | “集合相减”：匹配 `b`，不匹配 `e` | 解析为字符类 `[a-z\-\[aeiou]` 再接一个字面 `]`；单独的 `b` **不**匹配 |
| 后向断言中的带量词捕获 | `(?<=(\w){3})def` | 组 1 = `"a"` | 组 1 = `"c"`（整段匹配一致） |
| 后向断言中的反向引用 | `(?<=\1(\w))d` | 能匹配 | 能编译，但**不**匹配 |

### 本库支持、但 `dlclark/regexp2` 不支持（PCRE2 的额外能力）

占有量词 `a++`、原子组 `(?>…)`、递归 `(?R)`、`\K`、子程序调用 `(?&name)` 在本库都能
编译，而 `dlclark` 会拒绝。依赖这些特性的模式**无法**回迁到 `regexp2`。

## 安全性：灾难性回溯有界

与 `dlclark/regexp2`（默认 `MatchTimeout` 为“永不超时”）不同，本库的每次匹配都由
PCRE2 的 match/depth 限制兜底。经典的指数级 ReDoS（如 `(a+)+$` 对 `"aaaa…!"`）在默认
限制下约 120 ms 返回 `ErrMatchLimit`，配合 `SetMatchLimits(50000, …)` 约 0.6 ms 返回；
既不会卡死，也不会爆栈（PCRE2 10.x 在堆上完成匹配）。

本库针对真实世界的 JS 生态 ReDoS CVE 做了测试（moment.js CVE-2022-31129、
Cloudflare-2019、CWE-1333、UAParser.js CVE-2020-7733）：全部会终止。需要注意一点 ——
match 限制约束的是**指数级**回溯，而非**多项式**（如二次方）扫描；对多项式模式，真正
有效的防御是限制输入长度。

## 性能

测试环境 `go test -bench`、`darwin/arm64`（Apple M 系列）。在相同负载下对比的后端：
`dlclark` = 被替换的引擎，**drop-in** = 本库的 `regexp2` 兼容层（rune 输出），
**low-level** = 字节 API；`std` = Go 标准库 `regexp`（RE2），在其语法支持的场景下列出。

![相对 dlclark/regexp2 的吞吐](assets/bench-speedup.png)

drop-in 层在所有基准上都快于 `dlclark/regexp2`，低层字节 API 更快（1.6x–9.6x）。
升级到 PCRE2 10.47 后 cgo 匹配路径明显更快（布尔匹配从约 1100 ns 降到约 680 ns，
而作为对照的纯 Go `dlclark` 基本不变）。

| 场景 | dlclark | drop-in | low-level | 提速 | drop-in 分配 |
|---|---|---|---|---|---|
| 布尔匹配，短字符串 | 6472 ns | 676 ns | 674 ns | **9.6x** | 0 B / 0 |
| 布尔匹配，100 KB 输入 | 26.4 ms | 2.84 ms | 2.84 ms | **9.3x** | 0 B / 0 |
| 含反向引用的匹配 | 396 ns | 186 ns | 185 ns | **2.1x** | 0 B / 0 |
| 回溯密集，32 KB | 20.0 ms | 10.9 ms | 11.0 ms | **1.8x** | 0 B / 0 |
| Unicode `\p{Han}`，8 KB | 15.2 µs | 12.2 µs | 4.95 µs | 1.2x | 0 B / 0 |
| 含 6 个捕获的 Find | 1072 ns | 984 ns | 690 ns | 1.1x | 752 B / 7 |
| 编译（复杂模式） | 10.3 µs | ~3.2 µs | 3.16 µs | **3.3x** | 1.5 KB / 17 |
| 全量 Find-all，670 个匹配 | 380 µs | 138 µs | 63 µs | **2.8x**（ll 6.0x） | 193 KB / 2004 |
| 全量 Find-all，3 万个匹配 | 6.06 ms | 5.24 ms | 2.88 ms | **1.2x**（ll 2.1x） | 7.8 MB / 90k |

![各场景匹配时延](assets/bench-latency.png)

**布尔匹配在热路径上零分配**（0 B / 0 allocs）。

### 针对“海量小匹配”的优化

在大文本上遍历海量极小匹配，曾经是本引擎唯一输给纯 Go 回溯器的场景。根因并不是 cgo
边界，而是 **O(n²) 的 UTF-8 校验**：开启 `PCRE2_UTF` 后，PCRE2 在每次 `pcre2_match`
调用时都会重新校验**整段**输入，于是在 N 字节文本上做 N 次匹配的代价是 O(N²)。

两项改动解决了它：

1. **批量化 `FindAll` / 迭代** —— 用单个 C 函数（`p2l_match_all`）在一次 cgo 调用里
   收集一批匹配，把 N 次往返降为 ⌈N/256⌉ 次，并且每批只解码到一块底层切片
   （670 个匹配时，低层 `FindAll` 的分配次数从 676 降到 **14**）。
2. **只校验一次** —— 批量循环在首个匹配时校验 UTF-8，随后置 `PCRE2_NO_UTF_CHECK`，
   把 O(N²) 的重复校验收敛为 O(N)。

在 30 KB 文本上做 30000 个极小匹配的综合效果：低层 API 从 **171 ms 降到 2.9 ms**
（约 59 倍），drop-in 层从 **170 ms 降到 5.2 ms**，二者现在都**快于 `dlclark`
（6.1 ms）和 Go 的 `regexp`（5.5 ms）**。逐个匹配的迭代还改用了 PCRE2 10.47 新增的
官方 `pcre2_next_match()` 辅助函数，替换掉先前手写的空匹配推进逻辑。

![海量小匹配：优化前后对比](assets/bench-tiny-optimization.png)

批量路径还大幅削减了堆分配 —— 这对长时间运行服务的 GC 压力很重要：

![每次操作的堆分配次数](assets/bench-allocs.png)

### ReDoS 成本

`(a+)+$` 对 40 字符的对抗性输入：

| 限制 | 结果 |
|---|---|
| `dlclark`，无超时 | 卡死（灾难性） |
| 默认 match 限制 | 约 120 ms 返回 `ErrMatchLimit`（有界） |
| `SetMatchLimits(50000, …)` | 约 0.6 ms 返回 `ErrMatchLimit` |

复现数据并重新生成图表：

```bash
CGO_ENABLED=1 go test -bench . -benchmem -run '^$' .   # 运行基准
python3 tools/benchviz/plot.py                          # 重新生成 assets/*.png
```

## 兼容性验证

- **与 `dlclark/regexp2` 对齐：** 在 PCRE2 官方 1585 条输入的 `testoutput1` 语料上整段
  匹配 100% 一致，另有针对替换、完整迭代、分组访问的专门差分测试。
- **以 PCRE2 10.47 为基准真值：** 匹配结果在 PCRE2 自带 `testoutput2`/`testoutput4`
  语料上达成 929/931（8 位）与 1502/1504（UTF）一致；编译接受/拒绝在 1258/1258 接受、
  385/388 拒绝上一致（少数未命中是语料解析器对边界用例的近似，并非引擎缺陷）。
- **逐版本行为钉子（`pcre2_1047_regression_test.go`）：** 10.44–10.47 每一条影响行为的
  changelog 条目都以该版本的 `pcre2test` 黄金输出为准进行断言 —— 包括变长后向断言首分支
  修复、`\X` 需 ZWJ 才能连接两个 Extended_Pictographic 的字素簇断点、扫描子串断言
  `(*scs:)` / `(*scan_substring:)`、`(*ACCEPT)` 在 `(*scs:)` 内的 CVE-2025-58050 内存安全
  修复、10.47 新增的「带回捕获的子程序调用」`(?N(group,...))`、`pcre2_next_match` 对空匹配 /
  `\K` 匹配的迭代、UCD 16 属性、提升后的 128 字符命名组上限（在 128/129 处做边界测试），
  以及一条守护 10.47 命名组哈希表查找保持 O(1) 的回归测试。
- **JavaScript / Node：** ECMAScript `test262` 与 V8 的后向断言 / 命名组 / Unicode
  属性用例，外加真实 ReDoS CVE 安全测试。

## 测试

```bash
CGO_ENABLED=1 go test ./...            # 单元 + 差分 + 语料 + 安全
CGO_ENABLED=1 go test -race ./...      # 无数据竞争
CGO_ENABLED=1 go test -bench . ./...   # 对比 dlclark 与标准库 regexp 的基准
```

## 重新生成 vendoring 的 PCRE2 源码

```bash
./tools/generate-pcre2lite/generate.sh   # 下载、裁剪、配置（禁用 JIT）
./tools/verify-generated/verify.sh       # 校验已提交的源码可复现
```

## 许可证

内嵌的 PCRE2 C 源码采用 PCRE2 许可证（BSD 风格）；见
[THIRD_PARTY_LICENSES/PCRE2-LICENSE](THIRD_PARTY_LICENSES/PCRE2-LICENSE) 以及
`internal/pcre2lite/` 下的头文件。

本项目自有的 Go 与 wrapper 代码尚未选定许可证 —— 发布前请补充 `LICENSE` 文件。
