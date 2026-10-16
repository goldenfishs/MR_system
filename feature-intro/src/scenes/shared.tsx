import { Audio } from "@remotion/media";
import {
  AbsoluteFill,
  Easing,
  Img,
  interpolate,
  staticFile,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import type { SlideScene } from "./story";

const FONT = "-apple-system, BlinkMacSystemFont, 'PingFang SC', sans-serif";

const BrandMark: React.FC<{ size: number }> = ({ size }) => (
  <Img
    src={staticFile("assets/mark.png")}
    style={{ width: size, height: size, objectFit: "contain" }}
  />
);

const Backdrop: React.FC = () => (
  <AbsoluteFill
    style={{
      background:
        "radial-gradient(ellipse at 82% 28%, rgba(23,98,129,.34), transparent 36%), radial-gradient(ellipse at 8% 86%, rgba(124,44,143,.20), transparent 42%), linear-gradient(118deg,#0b1220 0%,#10182a 55%,#11152a 100%)",
    }}
  >
    <div
      style={{
        position: "absolute",
        inset: 0,
        opacity: 0.2,
        backgroundImage:
          "linear-gradient(rgba(176,205,226,.08) 1px,transparent 1px),linear-gradient(90deg,rgba(176,205,226,.08) 1px,transparent 1px)",
        backgroundSize: "68px 68px",
        maskImage: "linear-gradient(90deg,transparent,black 42%,black 100%)",
      }}
    />
    <div
      style={{
        position: "absolute",
        width: 740,
        height: 740,
        borderRadius: "50%",
        border: "1px solid rgba(159,206,226,.1)",
        right: -300,
        top: -260,
      }}
    />
  </AbsoluteFill>
);

const TopBar: React.FC<{ scene: SlideScene; index: number }> = ({ scene, index }) => (
  <div
    style={{
      position: "absolute",
      left: 84,
      right: 84,
      top: 55,
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      zIndex: 5,
      color: "#f4f7fb",
      fontFamily: FONT,
    }}
  >
    <div style={{ display: "flex", alignItems: "center", gap: 17 }}>
      <BrandMark size={44} />
      <div>
        <div style={{ fontSize: 21, fontWeight: 650, letterSpacing: 1 }}>
          青岛理工大学机器人战队
        </div>
        <div style={{ marginTop: 4, fontSize: 13, color: "#93a4b9", letterSpacing: 2 }}>
          CANGMING / MOVE
        </div>
      </div>
    </div>
    <div style={{ display: "flex", alignItems: "center", gap: 22 }}>
      <span style={{ fontSize: 13, color: "#91a4b8", letterSpacing: 2 }}>{scene.kicker}</span>
      <span style={{ fontSize: 17, fontVariantNumeric: "tabular-nums", color: "#d9e6f2" }}>
        {String(index + 1).padStart(2, "0")} <span style={{ color: "#718298" }}>/</span> 08
      </span>
    </div>
  </div>
);

const Title: React.FC<{ scene: SlideScene; frame: number }> = ({ scene, frame }) => (
  <div
    style={{
      position: "absolute",
      left: 92,
      top: 220,
      width: 610,
      zIndex: 3,
      opacity: interpolate(frame, [0, 22], [0, 1], {
        easing: Easing.bezier(0.16, 1, 0.3, 1),
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      }),
      translate: interpolate(frame, [0, 22], ["0px 34px", "0px 0px"], {
        easing: Easing.bezier(0.16, 1, 0.3, 1),
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      }),
      fontFamily: FONT,
    }}
  >
    <div
      style={{
        width: 58,
        height: 3,
        marginBottom: 24,
        borderRadius: 3,
        background: "linear-gradient(90deg,#e84c78,#a2dbd9)",
      }}
    />
    <div
      style={{
        color: "#f8fbff",
        fontSize: scene.visual === "brand" ? 86 : 74,
        fontWeight: 700,
        lineHeight: 1.11,
        letterSpacing: 1,
        whiteSpace: "pre-line",
        textShadow: "0 8px 38px rgba(0,0,0,.22)",
      }}
    >
      {scene.title}
    </div>
    <div
      style={{
        marginTop: 31,
        color: "#c1cfdd",
        fontSize: scene.visual === "brand" ? 28 : 25,
        fontWeight: 500,
        lineHeight: 1.52,
        maxWidth: 610,
      }}
    >
      {scene.lead}
    </div>
    <div
      style={{
        marginTop: 15,
        color: "#8495aa",
        fontSize: 20,
        lineHeight: 1.5,
        maxWidth: 590,
      }}
    >
      {scene.body}
    </div>
    <div style={{ display: "flex", flexWrap: "wrap", gap: 11, marginTop: 28, maxWidth: 610 }}>
      {scene.points.map((point, pointIndex) => (
        <div
          key={point}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 9,
            border: "1px solid rgba(190,215,232,.14)",
            background: "rgba(255,255,255,.045)",
            padding: "11px 14px",
            borderRadius: 999,
            color: "#b9c9d8",
            fontSize: 17,
            whiteSpace: "nowrap",
          }}
        >
          <span
            style={{
              width: 7,
              height: 7,
              borderRadius: 10,
              background: pointIndex === 0 ? "#e6537d" : "#67c6c5",
              boxShadow: "0 0 15px rgba(93,200,199,.45)",
            }}
          />
          {point}
        </div>
      ))}
    </div>
  </div>
);

const WindowFrame: React.FC<{
  scene: SlideScene;
  frame: number;
  durationInFrames: number;
  children: React.ReactNode;
}> = ({ scene, frame, durationInFrames, children }) => (
  <div
    style={{
      position: "absolute",
      left: 750,
      right: 84,
      top: 166,
      bottom: 145,
      padding: 14,
      overflow: "hidden",
      borderRadius: 26,
      border: "1px solid rgba(203,225,241,.28)",
      background: "linear-gradient(145deg,rgba(238,246,251,.12),rgba(194,222,238,.045))",
      boxShadow: "0 28px 90px rgba(0,0,0,.34),inset 0 1px rgba(255,255,255,.12)",
      opacity: interpolate(frame, [0, 24], [0, 1], {
        easing: Easing.bezier(0.16, 1, 0.3, 1),
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      }),
      scale: interpolate(frame, [0, durationInFrames], [0.975, 1.018], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
        output: "perceptual-scale",
      }),
      fontFamily: FONT,
    }}
  >
    <div
      style={{
        height: 49,
        display: "flex",
        alignItems: "center",
        padding: "0 15px",
        gap: 8,
        color: "#26354a",
        borderRadius: "16px 16px 0 0",
        background: "#f8fbfd",
      }}
    >
      <span style={{ width: 9, height: 9, borderRadius: 9, background: "#ee6b7e" }} />
      <span style={{ width: 9, height: 9, borderRadius: 9, background: "#f0bc64" }} />
      <span style={{ width: 9, height: 9, borderRadius: 9, background: "#63c79a" }} />
      <span style={{ marginLeft: 14, color: "#7b8794", fontSize: 14 }}>
        {scene.visual === "brand" ? "沧溟 / MOVE · 战队协作平台" : scene.visual === "signup" ? "流程示意 · 团队报名" : "沧溟 / MOVE · 功能界面"}
      </span>
      <span style={{ marginLeft: "auto", color: "#8997a4", fontSize: 12 }}>FUNCTION PREVIEW</span>
    </div>
    <div
      style={{
        position: "absolute",
        left: 14,
        right: 14,
        top: 63,
        bottom: 14,
        overflow: "hidden",
        borderRadius: "0 0 14px 14px",
        background: "#f5f7fa",
      }}
    >
      {children}
    </div>
  </div>
);

const BrandVisual: React.FC<{ frame: number }> = ({ frame }) => (
  <div style={{ position: "absolute", inset: 0, overflow: "hidden", borderRadius: 15 }}>
    <div
      style={{
        position: "absolute",
        width: 570,
        height: 570,
        borderRadius: "50%",
        border: "1px solid rgba(81,175,194,.23)",
        left: "50%",
        top: "49%",
        translate: "-50% -50%",
        scale: interpolate(frame, [0, 90], [0.94, 1.04], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
          output: "perceptual-scale",
        }),
      }}
    />
    <div
      style={{
        position: "absolute",
        width: 420,
        height: 420,
        borderRadius: "50%",
        border: "1px solid rgba(215,94,134,.28)",
        left: "50%",
        top: "49%",
        translate: "-50% -50%",
      }}
    />
    <div
      style={{
        position: "absolute",
        left: "50%",
        top: "47%",
        translate: "-50% -50%",
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        width: 250,
        height: 250,
        borderRadius: 50,
        background: "linear-gradient(145deg,rgba(244,249,252,.98),rgba(217,232,240,.92))",
        boxShadow: "0 28px 90px rgba(4,10,20,.45),0 0 85px rgba(71,187,196,.17)",
        rotate: interpolate(frame, [0, 180], ["-2deg", "2deg"], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
        }),
      }}
    >
      <BrandMark size={144} />
    </div>
    <div
      style={{
        position: "absolute",
        left: "50%",
        top: "78%",
        translate: "-50% 0",
        color: "#a7bfce",
        fontSize: 16,
        letterSpacing: 5,
        whiteSpace: "nowrap",
      }}
    >
      ONE TEAM · TWO COMPETITIONS
    </div>
    <ContestCard name="RoboCon" eyebrow="NATIONAL ROBOT CONTEST" left={80} top={115} />
    <ContestCard name="RoboMaster" eyebrow="ROBOMASTER" right={58} bottom={105} accent="#ed7395" />
    <div
      style={{
        position: "absolute",
        left: "50%",
        top: "18%",
        width: 520,
        height: 1,
        translate: "-50% 0",
        background: "linear-gradient(90deg,transparent,rgba(145,211,220,.34),transparent)",
      }}
    />
  </div>
);

const ContestCard: React.FC<{
  name: string;
  eyebrow: string;
  left?: number;
  right?: number;
  top?: number;
  bottom?: number;
  accent?: string;
}> = ({ name, eyebrow, left, right, top, bottom, accent = "#6fbdc5" }) => (
  <div
    style={{
      position: "absolute",
      left,
      right,
      top,
      bottom,
      padding: "19px 25px",
      border: "1px solid rgba(214,230,242,.22)",
      borderRadius: 14,
      background: "rgba(13,26,42,.86)",
      boxShadow: "0 14px 38px rgba(0,0,0,.23)",
      color: "#edf6fb",
    }}
  >
    <div style={{ fontSize: 11, color: accent, letterSpacing: 2 }}>{eyebrow}</div>
    <div style={{ marginTop: 7, fontSize: 24, fontWeight: 600 }}>{name}</div>
  </div>
);

const SignupVisual: React.FC = () => (
  <div
    style={{
      position: "absolute",
      inset: 0,
      display: "flex",
      justifyContent: "center",
      alignItems: "center",
      padding: 45,
      background: "linear-gradient(140deg,#eef3f7,#e7edf3)",
      fontFamily: FONT,
    }}
  >
    <div
      style={{
        width: 680,
        padding: 36,
        borderRadius: 18,
        background: "white",
        boxShadow: "0 18px 50px rgba(27,46,65,.12)",
        color: "#26374a",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 15, marginBottom: 24 }}>
        <div
          style={{
            width: 52,
            height: 52,
            borderRadius: 15,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            background: "#fff1f5",
          }}
        >
          <BrandMark size={34} />
        </div>
        <div>
          <div style={{ fontSize: 24, fontWeight: 650 }}>团队报名</div>
          <div style={{ marginTop: 5, fontSize: 14, color: "#8391a1" }}>
            青岛理工大学机器人战队 · 沧溟 / MOVE
          </div>
        </div>
        <div
          style={{
            marginLeft: "auto",
            padding: "8px 12px",
            borderRadius: 99,
            background: "#edf7f4",
            color: "#33826d",
            fontSize: 13,
          }}
        >
          招募开放
        </div>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 14 }}>
        <MockField label="姓名" value="填写真实姓名" />
        <MockField label="学号" value="填写学号" />
        <MockField label="专业 / 班级" value="选择或填写" />
        <MockField label="意向组别" value="机械 · 电控 · 硬件" />
      </div>
      <div
        style={{
          marginTop: 15,
          border: "1px dashed #bccbd9",
          background: "#f8fafc",
          borderRadius: 12,
          padding: "17px 20px",
          display: "flex",
          alignItems: "center",
          gap: 14,
        }}
      >
        <div style={{ fontSize: 24, color: "#4880bd" }}>⇧</div>
        <div>
          <div style={{ color: "#34465b", fontSize: 16, fontWeight: 600 }}>上传个人简历</div>
          <div style={{ marginTop: 5, fontSize: 13, color: "#8693a2" }}>PDF 文件 · 用于报名审核</div>
        </div>
        <span style={{ marginLeft: "auto", fontSize: 13, color: "#8594a5" }}>PDF / MAX 10 MB</span>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 18, marginTop: 23 }}>
        <div style={{ flex: 1, height: 1, background: "#e8edf2" }} />
        <span style={{ color: "#9ba8b5", fontSize: 13 }}>报名审核流程</span>
        <div style={{ flex: 1, height: 1, background: "#e8edf2" }} />
      </div>
      <div style={{ display: "flex", justifyContent: "center", gap: 14, marginTop: 18 }}>
        {[
          ["01", "提交报名"],
          ["02", "管理审核"],
          ["03", "新生培养"],
        ].map(([number, label], index) => (
          <div key={number} style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <div
              style={{
                width: 34,
                height: 34,
                borderRadius: 50,
                display: "grid",
                placeItems: "center",
                fontSize: 12,
                color: index === 2 ? "#477cb3" : "#fff",
                background: index === 2 ? "#e7eff8" : "#4d87c0",
              }}
            >
              {number}
            </div>
            <span style={{ color: "#617185", fontSize: 15 }}>{label}</span>
            {index < 2 && <span style={{ color: "#aab6c0", fontSize: 20, margin: "0 8px" }}>›</span>}
          </div>
        ))}
      </div>
    </div>
  </div>
);

const MockField: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div style={{ border: "1px solid #e2e8ee", borderRadius: 10, padding: "11px 14px" }}>
    <div style={{ color: "#8492a1", fontSize: 12 }}>{label}</div>
    <div style={{ marginTop: 6, color: "#4c5e70", fontSize: 15 }}>{value}</div>
  </div>
);

const ScreenshotVisual: React.FC<{ scene: SlideScene; frame: number }> = ({ scene, frame }) => (
  <div
    style={{
      position: "absolute",
      inset: 0,
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
      background: "#f1f3f6",
      overflow: "hidden",
    }}
  >
    {scene.asset ? (
      <Img
        src={staticFile(scene.asset)}
        style={{
          width: "100%",
          height: "100%",
          objectFit: scene.fit ?? "contain",
          objectPosition: scene.position ?? "center",
          scale: interpolate(frame, [0, 240], [1, 1.04], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            output: "perceptual-scale",
          }),
        }}
      />
    ) : null}
  </div>
);

const Progress: React.FC<{ index: number }> = ({ index }) => (
  <div
    style={{
      position: "absolute",
      left: 92,
      right: 92,
      bottom: 36,
      display: "flex",
      alignItems: "center",
      gap: 15,
      color: "#718398",
      fontFamily: FONT,
      fontSize: 13,
      letterSpacing: 1.5,
      zIndex: 6,
    }}
  >
    <span>FEATURE TOUR</span>
    <div style={{ flex: 1, height: 2, background: "rgba(198,218,234,.14)" }}>
      <div
        style={{
          width: `${((index + 1) / 8) * 100}%`,
          height: "100%",
          background: "linear-gradient(90deg,#e5557d,#68c6c2)",
        }}
      />
    </div>
    <span>QUT ROBOT TEAM · 2027</span>
  </div>
);

export const FeatureSlide: React.FC<{
  scene: SlideScene;
  index: number;
  durationInFrames: number;
}> = ({ scene, index, durationInFrames }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const fadeStart = Math.max(8, durationInFrames - Math.round(0.32 * fps));

  return (
    <AbsoluteFill>
      <Backdrop />
      <TopBar scene={scene} index={index} />
      <Title scene={scene} frame={frame} />
      <WindowFrame scene={scene} frame={frame} durationInFrames={durationInFrames}>
        {scene.visual === "brand" ? (
          <BrandVisual frame={frame} />
        ) : scene.visual === "signup" ? (
          <SignupVisual />
        ) : (
          <ScreenshotVisual scene={scene} frame={frame} />
        )}
      </WindowFrame>
      <Progress index={index} />
      <Audio
        src={staticFile(`audio/${scene.id}.mp3`)}
        volume={(audioFrame) =>
          interpolate(audioFrame, [0, 8, fadeStart, durationInFrames], [0, 1, 1, 0], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          })
        }
      />
    </AbsoluteFill>
  );
};
