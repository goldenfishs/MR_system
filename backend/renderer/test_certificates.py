"""Document import regression tests; run with the renderer's Python runtime."""
import tempfile
import unittest
from pathlib import Path
import zipfile

from certificates import secure_docx


class DocxImportTests(unittest.TestCase):
    def document(self, files):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        path = Path(directory.name) / 'template.docx'
        with zipfile.ZipFile(path, 'w') as archive:
            archive.writestr('word/document.xml', '<document/>')
            for name, content in files.items():
                if name != 'word/document.xml':
                    archive.writestr(name, content)
        if 'word/document.xml' in files:
            # Avoid duplicate ZIP members; real imports are inspected member by
            # member, but fixture generation should retain ordinary OOXML form.
            with zipfile.ZipFile(path, 'w') as archive:
                for name, content in files.items():
                    archive.writestr(name, content)
        return path

    def test_static_document_with_internal_picture(self):
        path = self.document({
            'word/_rels/document.xml.rels': '<Relationships><Relationship Target="media/photo.png"/></Relationships>',
            'word/media/photo.png': b'fixture image',
        })
        secure_docx(path)

    def test_external_relationship_encodings_are_rejected(self):
        for xml in [
            '<Relationships><Relationship TargetMode="Exter&#110;al" Target="file:///etc/passwd"/></Relationships>',
            '<Relationships><Relationship TargetMode="External" Target="https://example.test/a"/></Relationships>'.encode('utf-16'),
            '<Relationships><Relationship Target="file%3A///etc/passwd"/></Relationships>',
            '<Relationships><Relationship Target="../../../etc/passwd"/></Relationships>',
            '<Relationships xmlns:x="urn:fake"><Relationship Target="local.txt" TargetMode="External" x:TargetMode="Internal"/></Relationships>',
        ]:
            with self.subTest(xml=xml):
                path = self.document({'word/_rels/document.xml.rels': xml})
                with self.assertRaises(ValueError):
                    secure_docx(path)

    def test_external_word_fields_and_embedded_content_are_rejected(self):
        for xml in [
            '<document><instrText>INCLUDETEXT "file:///etc/passwd"</instrText></document>',
            '<document><instrText>INCL</instrText><instrText>UDETEXT "file:///etc/passwd"</instrText></document>',
            '<document><fldSimple instr="DDEAUTO some-command"/></document>',
            '<document><altChunk/></document>',
            '<!DOCTYPE doc [<!ENTITY data "expanded">]><document>&data;</document>',
        ]:
            with self.subTest(xml=xml):
                path = self.document({'word/document.xml': xml})
                with self.assertRaises(ValueError):
                    secure_docx(path)

    def test_standard_page_and_date_fields_are_retained(self):
        secure_docx(self.document({'word/document.xml': '<document><instrText>PAGE</instrText><fldSimple instr="DATE"/></document>'}))


if __name__ == '__main__':
    unittest.main()
