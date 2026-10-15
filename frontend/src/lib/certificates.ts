export type CertificateKey = 'name' | 'direction' | 'major' | 'className' | 'studentNumber' | 'date' | 'text';
export interface CertificateField {
  id: string; key: CertificateKey; text?: string; page: number;
  x: number; y: number; width: number; fontSize: number;
  align: 'left' | 'center' | 'right'; color: string;
}
export interface CertificateTemplate {
  id: string; name: string; sourceFileId: string; pdfFileId: string;
  pages: { width: number; height: number; previewFileId: string }[];
  fields: CertificateField[]; createdAt: string; updatedAt: string; createdBy: string;
}
export const certificateKeys: {title:string;value:CertificateKey}[] = [
  {title:'姓名',value:'name'},{title:'组别',value:'direction'},{title:'学号',value:'studentNumber'},
  {title:'专业',value:'major'},{title:'班级',value:'className'},{title:'发放日期',value:'date'},{title:'自定义文字',value:'text'},
];
