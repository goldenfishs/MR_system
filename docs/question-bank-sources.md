# 入门题库的来源与编写说明

本题库为 QUT 机器人战队的入门练习内容。题干、选项、数值、答案解析与 SVG 电路图均为本项目独立编写；参考公开可阅读的芯片厂商手册、应用笔记和基础教程核对技术事实，**并非复制第三方题库或历年考试题**。来源链接用于进一步学习，不代表上游机构为题目背书。项目不随题库再分发第三方手册或教程全文；其著作权和许可仍归原作者。

题目带有稳定 ID。重复导入同一 ID 应跳过已存在的题目，避免覆盖管理员后续修改。练习结果只是题目作答记录，不自动认定阶段考核通过或培训结业。

## 电控组：60 题

内容文件：`backend/question_banks/electronics.json` 中 `mcu-001` 至 `mcu-024`，方向内部值为 `电控`。统一归入“单片机通用基础”，覆盖 GPIO、位操作、中断与共享数据、定时器、PWM、ADC、UART、I²C、SPI、CAN、DMA、看门狗和按键去抖。

新增内容文件：`backend/question_banks/c_language.json`，共 36 道原创题，按以下课程标签组织：

- **C语言基础**：16 题（14 道客观题、2 道编程题）。覆盖变量、类型、const、算术与逻辑运算、类型转换、printf、scanf 和 if/else。
- **C语言进阶**：12 题（10 道客观题、2 道编程题）。覆盖循环、数组、函数、指针、字符串、结构体及位运算。
- **STM32单片机基础**：8 道客观题。以 STM32F103 为明确对象，覆盖 RCC、GPIO、BSRR、EXTI、定时器、ADC、USART 和 DMA，不将 RP2040 或其他 STM32 系列的寄存器行为混用。

新增 C 题依据 [cppreference 的 C 语言参考](https://en.cppreference.com/w/c.html) 核对，每题附对应章节；STM32 题依据 ST 官方 [RM0008 参考手册](https://www.st.com/resource/en/reference_manual/cd00171190-stm32f101xx-stm32f102xx-stm32f103xx-stm32f105xx-and-stm32f107xx-advanced-armbased-32bit-mcus-stmicroelectronics.pdf)。这里的 C 题使用 C 语言语义，不使用 C++ 特性；编程题限定 Judge0 的 C（GCC 9.2.0，ID 50），包含 22 个公开与隐藏测试用例。题干与测试由本项目编写，不复制参考资料中的题目。

原有 24 道嵌入式基础题及其参考资料保留如下：

| 公开参考资料 | 题目与参考范围 |
| --- | --- |
| Raspberry Pi, [RP2040 Datasheet](https://datasheets.raspberrypi.com/rp2040/rp2040-datasheet.pdf) | `mcu-001`—`mcu-013`、`mcu-017`—`mcu-018`、`mcu-021`—`mcu-024`；GPIO、原子寄存器访问、NVIC、中断、DMA、UART、SPI、PWM、定时器、看门狗及 ADC 章节。 |
| NXP, [UM10204: I²C-bus specification and user manual, Rev. 7.0](https://www.nxp.com/docs/en/user-guide/UM10204.pdf) | `mcu-014`—`mcu-016`；开漏与上拉、START/STOP、ACK/NACK，主要为第 3.1 节。 |
| Texas Instruments, [SLOA101B: Introduction to the Controller Area Network (CAN)](https://www.ti.com/lit/an/sloa101b/sloa101b.pdf) | `mcu-019`—`mcu-020`；经典 CAN 标识符仲裁、高速 CAN 差分传输与终端匹配。 |
| Texas Instruments, [SLUA618A: Fundamentals of MOSFET and IGBT Gate Driver Circuits](https://www.ti.com/lit/an/slua618a/slua618a.pdf) | `mcu-002` 补充；GPIO 逻辑控制与功率开关驱动不能混为一谈。 |
| SparkFun, [Capacitors](https://learn.sparkfun.com/tutorials/capacitors/all) | `mcu-009`、`mcu-024` 补充；滤波与 RC 网络的基础解释。 |

适用范围：题目讲解通用基础；引用 RP2040 不代表所有 MCU 的寄存器名称、ADC 误差、GPIO 电压容限或外设行为都相同。计算题显式给出时钟、计数范围和量化公式，避免套用不同芯片实现。中断、去抖和看门狗题是基于机制的工程情景题，没有宣称手册原文包含同一道题。

## 硬件组：24 题

内容文件：`backend/question_banks/electronics.json` 中 `hw-001` 至 `hw-024`，方向内部值为 `硬件`。覆盖电阻色环与贴片标记、串并联、功率、电容标记与极性、去耦、电感、二极管、LED、MOSFET、运放、分压、RC、电源、逻辑门和万用表。

| 公开参考资料 | 题目与参考范围 |
| --- | --- |
| SparkFun, [Resistors](https://learn.sparkfun.com/tutorials/resistors/all) | `hw-001`—`hw-004`、`hw-011`；色环、三位数字标记、电阻串并联、欧姆定律、额定功率。 |
| SparkFun, [Capacitors](https://learn.sparkfun.com/tutorials/capacitors/all) | `hw-005`—`hw-007`、`hw-020`—`hw-021`；电容值与类型、极性、去耦和一阶 RC 基础。 |
| Coilcraft, [What Is an Inductor?](https://www.coilcraft.com/en-us/edu/series/what-is-an-inductor/) | `hw-008`；电感的磁场储能与电流变化关系。 |
| SparkFun, [Diodes](https://learn.sparkfun.com/tutorials/diodes/all) | `hw-009`—`hw-011`；正反向、极性标记、整流与发光二极管。 |
| Texas Instruments, [SLUA618A: Fundamentals of MOSFET and IGBT Gate Driver Circuits](https://www.ti.com/lit/an/slua618a/slua618a.pdf) | `hw-009`、`hw-012`—`hw-014`；感性开关瞬态、VGS、阈值电压、栅极电荷与驱动。 |
| Texas Instruments, [SLOA011: Understanding Operational Amplifier Specifications](https://www.ti.com/lit/pdf/sloa011) | `hw-015`—`hw-017`；理想运放、同相放大器、负反馈、共模与输出摆幅限制。 |
| SparkFun, [Voltage Dividers](https://learn.sparkfun.com/tutorials/voltage-dividers/all) | `hw-018`—`hw-019`；空载分压与负载效应。 |
| Texas Instruments, [SLVA079: Understanding the Terms and Definitions of LDO Voltage Regulators](https://www.ti.com/lit/an/slva079/slva079.pdf) | `hw-022`；线性稳压器耗散与结温，第 12 节。 |
| Texas Instruments, [SN74HC00 Datasheet](https://www.ti.com/lit/ds/symlink/sn74hc00.pdf) | `hw-023`；两输入 NAND 真值表。 |
| SparkFun, [How to Use a Multimeter](https://learn.sparkfun.com/tutorials/how-to-use-a-multimeter/all) | `hw-024`；电阻与通断测量、测量前断电。 |

三个原创示意图位于 `frontend/public/question-bank/`：

- `resistor-bands.svg`：四环电阻（黄、紫、红、金），同时标注颜色文字。
- `voltage-divider.svg`：空载电阻分压，明确阻值与电源。
- `rc-lowpass.svg`：串联 R、对地 C 的无源一阶网络，明确高阻负载。

图中电路只用于题目中的理想化分析，不是可直接制造的完整电气设计。每题的 `sourceUrls` 随题保留，来源核验日期为 2026-10-15。

## 算法组：32 题

内容文件：`backend/question_banks/algorithms.json`。24 道客观题涵盖 C++ 基础（8 题）、Python 基础（8 题）和数据结构与算法基础（8 题）；另有 8 道支持 C++17 / Python 3 的编程题，覆盖求和、最大公约数、二分查找、括号匹配、前缀和、最大子段和、栅格最短路、去重排序，共 36 个公开与隐藏测试用例。

- [Python 官方教程](https://docs.python.org/3/tutorial/)：核对 `range`、列表、切片、别名、字典、集合与控制流的语义。
- [cppreference](https://en.cppreference.com/w/cpp.html)：核对 C++ 整数运算、数组、引用、函数、`vector` 和 `lower_bound` 的定义。
- [TheAlgorithms/C-Plus-Plus](https://github.com/TheAlgorithms/C-Plus-Plus) 与 [TheAlgorithms/Python](https://github.com/TheAlgorithms/Python)：提供二分查找、BFS、前缀和、数论、动态规划的开放参考实现入口。

题目中的机器人采样、地图情境、数值、选项、解析和测试用例为本项目独立编写，没有复制以上项目的源码、原题陈述或文档段落。每道题的参考链接在 JSON 中保留，首个链接通过题目「参考资料」提供。所有链接在 2026-10-15 检查可访问。运行 `python3 backend/question_banks/verify_programming.py` 可复核全部编程测试的预期输出；它不代表线上 Judge0 编译服务已部署。

## 导入及使用

管理员在对应组的「题库 → 导入基础题库」选择题库并确认。系统使用稳定题目 ID，导入在一个事务中完成；重复导入跳过已有题目，不覆盖标题、答案、管理员修改或作答记录。删除未有作答记录的内置题目后，重新导入可恢复该题。不会在每次启动时自动重新导入。导入界面会补充已有电控题缺失的课程标签，但不会覆盖教师已有的课程分类，也不会改动题面或作答记录。

三组共 116 题：104 道客观题可直接在线判分，12 道编程题配有测试用例。编程题在线编译依赖独立 Judge0 服务，未配置时页面明确提示，可先阅读题目并在本地练习。所有题目属于自主题库练习，不创建作业、考试、阶段淘汰或结业记录；所有组成员均可跨组查看和练习题库；作业、考试仍按相应成员资格和任务权限控制。


## 算法阶梯新增练习（2026-10-18）

已通过管理 API 添加 24 道原创编程题至算法组公开题库，第一阶段编程入门 4 题、第二阶段数组与容器 8 题、第三阶段贪心与搜索 6 题、第四阶段动态规划 6 题，共 168 个测试用例，支持 C++17 与 Python 3。内容文件及来源核查记录见 `content/algorithm-progressive-sources.md`。本批次不属于上述内置 116 题统计，不改写既有题库、内部培训题、作业或作答。

## 算法系统学习路径：新增 180 题（2026-10-18）

按用户要求由三个 GPT-6.1-sol 子代理分别编写基础、数据结构和进阶算法各 60 题，父代理统一查重、复核、发布。12 章各 15 题，覆盖输入输出、分支、循环数论、数组矩阵、字符串、递归、排序二分、栈队列集合、前缀和双指针、贪心动态规划、搜索回溯、图论综合。题面、数据和维护参考解答独立编写，不复制 SkyWalker 题库；相关背景核查见前述算法阶梯来源记录。

内容在 `content/curriculum/`，每题支持 C++17 / Python 3，含两份公开样例与隐藏测试，共 1527 个去重固定测试（360 公开、1167 隐藏）。参考解答保存在维护文件，不随导入发送给学生。已上线为公开练习，原有 56 道公开题保留，合计 236 道公开题；15 道内部视觉培训题保持内部可见。

题库按 01–12 章及题号排序，可再按知识标签筛选，作业仍由管理员自行选题发布。本次未自动创建作业或修改已有提交。最终内容指纹、导入 ID、独立校验、在线抽测及限制见 `content/curriculum/release-verification.json` 和各报告。

## 电控系统学习路径：新增 120 题（2026-10-18 编写）

三个 GPT-6.1-sol 子代理分别编写 C 基础、C 进阶和 STM32 各 40 题，父代理审阅、修正重复学习目标和歧义、运行全量 C 消毒器与本地 API 判分验证。12 章各 10 题，共 40 道 C 编程、31 道单选、23 道多选、26 道填空；内容、生成器、独立验证与发布记录在 `content/control-curriculum/`。

本批题面、数值、选项、解析和测试均独立编写；参考资料用于核对语言语义和芯片机制，没有搬运第三方题库。C 编程题限定 C11 可移植语义，涉及字长、取整、字符编码、字节序、CRC 或通信规则时在题面明确条件。每题保存参考来源和核验方法。

STM32 部分明确针对 STM32F103C8 / Cortex-M3，官方原文已下载并提取核对：RM0008 Rev21、PM0056 Rev7、DS5319 Rev20、UM2237 Rev31。逐题章节、页码、61 处文本证据及源 PDF 哈希记录于 `stm32-report.json`；共 15 项独立公式检查。参考资料是芯片机制依据，不代表 ST 对题库的授权或背书，也不代表硬件实测。

课程分组沿用“C语言基础 / C语言进阶 / STM32单片机基础”，章节可通过知识标签筛选。原有 60 道电控题保留，本批按只追加策略发布为公开练习，其他组成员也可刷题。最终线上数量和核验状态以 `content/control-curriculum/release-verification.json` 为准。
