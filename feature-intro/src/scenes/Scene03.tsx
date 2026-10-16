import { FeatureSlide } from "./shared";
import { SCENES } from "./story";

export const Scene03: React.FC<{ durationInFrames: number }> = ({ durationInFrames }) => (
  <FeatureSlide scene={SCENES[2]} index={2} durationInFrames={durationInFrames} />
);
