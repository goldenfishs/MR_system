# 沧溟 / MOVE 功能介绍视频

这是 MR System 的中文功能介绍视频工程。Remotion 源码、界面截图、旁白音频和中文字幕都保存在本目录；渲染后的 MP4 写入 `../docs/media/cangming-move-feature-tour.mp4`。

## 本地预览与渲染

需要 Node.js 22+、npm、Python 3 和 FFmpeg。首次安装与打开预览：

```sh
npm ci
npm run dev
```

在 macOS 上修改旁白后，重新生成系统中文语音、时间轴与字幕，再渲染：

```sh
npm run voiceover
npm run render
```

语音默认使用 macOS 的普通话 `Tingting` 声线。可以用 `MR_TTS_VOICE` 和 `MR_TTS_RATE` 环境变量替换声线和语速。渲染前运行 `npm run lint` 检查 TS/React 源码。

## 内容与边界

视频讲解团队报名、新生阶段考核、五组学习空间、作业评阅、讨论区、个人荣誉中心和分组权限。截图来自项目演示界面；报名表单场景是流程示意图。

邮箱验证码注册与批量邮件依赖已配置的 SMTP 服务；在线代码评测需另行配置 Judge0 兼容服务，当前演示环境未接入源码执行。视频旁白已保留这些配置说明。

旁白文本维护在 `src/scenes/story.json`，每个场景组件位于 `src/scenes/Scene*.tsx`。运行 `npm run voiceover` 会更新 `public/audio/`、`public/captions.json`、`public/zh-CN.srt` 与 `src/scenes/generated-timing.json`。
