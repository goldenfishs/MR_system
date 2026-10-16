import storyJson from "./story.json";

export type SlideScene = {
  id: string;
  title: string;
  kicker: string;
  lead: string;
  body: string;
  points: string[];
  visual: "brand" | "signup" | "screenshot";
  narration: string;
  asset?: string;
  fit?: "contain" | "cover";
  position?: string;
};

export const SCENES = storyJson as SlideScene[];
