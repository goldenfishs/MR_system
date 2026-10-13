export interface KnowledgeResource {
  id: string;
  title: string;
  description: string;
  direction: string;
  kind: "file" | "pdf" | "video";
  fileIds: string[];
  url: string;
  createdBy: string;
  createdAt: string;
}
