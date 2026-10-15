"""Private, bounded document rendering worker. JSON stdin/stdout, no shell commands."""
import json
import math
import os
from pathlib import Path
import posixpath
import re
import resource
import shutil
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
import zipfile
from urllib.parse import unquote

from PIL import Image, ImageOps
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

MAX_PAGES = 5
MAX_POINTS = 1440
DPI = 180
Image.MAX_IMAGE_PIXELS = 20000000


def command(args, timeout=45):
    try:
        return subprocess.run(args, check=True, capture_output=True, timeout=timeout).stdout
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
        raise ValueError('文件转换失败，请检查文件是否损坏或加密，Word 模板可另存为 PDF 后上传。') from exc


def secure_docx(source):
    with zipfile.ZipFile(source) as archive:
        entries = archive.infolist()
        if len(entries) > 2000 or sum(e.file_size for e in entries) > 80 * 1024 * 1024:
            raise ValueError('Word 模板内容过大，请精简图片后重试。')
        if 'word/document.xml' not in archive.namelist():
            raise ValueError('请上传有效的 DOCX 文件。')
        for entry in entries:
            name = entry.filename.lower()
            if entry.flag_bits & 1 or name.startswith('/') or '\\' in name or '..' in Path(name).parts:
                raise ValueError('Word 模板包含不支持的文件内容，请另存为 PDF。')
            if any(part in name for part in ['vbaproject', 'activex', 'embeddings/', 'oleobject']):
                raise ValueError('Word 模板不能包含宏或嵌入对象，请另存为 PDF。')
            if name.endswith(('.xml', '.rels')):
                data = archive.read(entry)
                # Inspect parsed values, not raw-byte attribute regexes: XML
                # character references and UTF-16 must not hide external URLs.
                encoding = 'utf-16' if data.startswith((b'\xff\xfe', b'\xfe\xff')) else 'utf-8-sig'
                try:
                    xml = data.decode(encoding)
                except UnicodeError as exc:
                    raise ValueError('Word 模板 XML 编码不受支持，请另存为 PDF。') from exc
                if '\x00' in xml or re.search(r'<!\s*(DOCTYPE|ENTITY)', xml, re.I):
                    raise ValueError('Word 模板包含不支持的 XML 内容。')
                try:
                    root = ET.fromstring(xml)
                except ET.ParseError as exc:
                    raise ValueError('Word 模板 XML 内容损坏，请另存为 PDF。') from exc
                instructions = []
                for element in root.iter():
                    tag = element.tag.rsplit('}', 1)[-1].lower()
                    attrs = [(key.rsplit('}', 1)[-1].lower(), value) for key, value in element.attrib.items()]
                    if tag == 'relationship':
                        if any(key == 'targetmode' and value.strip().lower() == 'external' for key, value in attrs):
                            raise ValueError('Word 模板包含外部链接，请移除外部链接或另存为 PDF。')
                        for key, value in attrs:
                            if key != 'target':
                                continue
                            target = unquote(value.strip())
                            if re.match(r'^[a-z][a-z0-9+.-]*:', target, re.I) or target.startswith(('/', '\\')):
                                raise ValueError('Word 模板包含外部链接，请移除外部链接或另存为 PDF。')
                            parent = posixpath.dirname(posixpath.dirname(name))
                            resolved = posixpath.normpath(posixpath.join(parent, target.replace('\\', '/')))
                            if resolved == '..' or resolved.startswith('../'):
                                raise ValueError('Word 模板包含包外文件引用，请另存为 PDF。')
                    # Word field instructions can reference files without any
                    # relationship entry. Preserve ordinary PAGE/DATE fields.
                    instruction = ''.join(element.itertext()) if tag == 'instrtext' else ''.join(value for key, value in attrs if key == 'instr')
                    instructions.append(instruction)
                    if re.search(r'\b(?:INCLUDETEXT|INCLUDEPICTURE|DDEAUTO|DDE|LINK|DATABASE)\b', instruction, re.I):
                        raise ValueError('Word 模板包含外部数据字段，请替换为静态文字或另存为 PDF。')
                    if tag in ('altchunk', 'oleobject', 'object'):
                        raise ValueError('Word 模板不能包含嵌入对象或外部内容，请另存为 PDF。')
                # A field keyword may be split across multiple styled runs.
                if re.search(r'INCLUDETEXT|INCLUDEPICTURE|DDEAUTO|DDE|LINK|DATABASE', ''.join(instructions), re.I):
                    raise ValueError('Word 模板包含外部数据字段，请替换为静态文字或另存为 PDF。')


def pdf_pages(source, work):
    info = command(['pdfinfo', str(source)]).decode('utf-8', 'replace')
    match = re.search(r'^Pages:\s*(\d+)', info, re.M)
    count = int(match.group(1)) if match else 0
    if not 1 <= count <= MAX_PAGES:
        raise ValueError('证书模板需为 1 至 5 页。')
    pages = []
    for index in range(count):
        detail = command(['pdfinfo', '-f', str(index + 1), '-l', str(index + 1), str(source)]).decode('utf-8', 'replace')
        size = re.search(r'(?:Page\s+\d+\s+size|Page size):\s*([\d.]+)\s+x\s+([\d.]+)', detail)
        if not size:
            raise ValueError('无法读取模板页面尺寸。')
        width, height = map(float, size.groups())
        if not 36 <= min(width, height) or max(width, height) > MAX_POINTS:
            raise ValueError('模板页面尺寸需在半英寸至 20 英寸之间。')
        rotation = re.search(r'(?:Page\s+\d+\s+rot|Page rot):\s*(\d+)', detail)
        if rotation and int(rotation.group(1)) % 180:
            width, height = height, width
        prefix = work / f'background-{index}'
        command(['pdftoppm', '-f', str(index + 1), '-l', str(index + 1), '-singlefile', '-r', str(DPI), '-png', str(source), str(prefix)])
        path = prefix.with_suffix('.png')
        with Image.open(path) as image:
            image.load()
            if image.width * image.height > 20000000:
                raise ValueError('模板图片分辨率过大。')
        pages.append((width, height, path))
    return pages


def load_pages(source, source_name, work):
    extension = Path(source_name).suffix.lower()
    if extension in ('.png', '.jpg', '.jpeg'):
        with Image.open(source) as image:
            image.load()
            image = ImageOps.exif_transpose(image).convert('RGB')
            # A4-sized longest side keeps image certificates printable by default.
            scale = min(842 / max(image.size), 1)
            width, height = image.width * scale, image.height * scale
            if min(width, height) < 36:
                raise ValueError('模板图片尺寸过小。')
            image.thumbnail((3508, 3508))
            target = work / 'background-0.png'
            image.save(target)
        return [(width, height, target)]
    if extension == '.docx':
        secure_docx(source)
        soffice = shutil.which('soffice') or shutil.which('libreoffice')
        if not soffice:
            raise ValueError('当前环境未安装 Word 转换服务，可先上传 PDF 或图片模板。')
        docx = work / 'template.docx'
        shutil.copyfile(source, docx)
        profile = work / 'office-profile'
        (profile / 'user').mkdir(parents=True)
        (profile / 'user' / 'registrymodifications.xcu').write_text('''<?xml version="1.0" encoding="UTF-8"?><oor:items xmlns:oor="http://openoffice.org/2001/registry"><item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item><item oor:path="/org.openoffice.Office.Writer/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>0</value></prop></item></oor:items>''')
        command([soffice, '-env:UserInstallation=' + profile.as_uri(), '--headless', '--nologo', '--nodefault', '--norestore', '--convert-to', 'pdf:writer_pdf_Export', '--outdir', str(work), str(docx)])
        source = work / 'template.pdf'
        if not source.is_file():
            raise ValueError('无法转换 Word 模板，请另存为 PDF 后重试。')
    elif extension != '.pdf':
        raise ValueError('模板支持 PDF、DOCX、PNG 和 JPEG。')
    return pdf_pages(source, work)


def setup_font():
    candidates = [os.getenv('CERTIFICATE_FONT', ''), '/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc', '/usr/share/fonts/wqy/wqy-zenhei.ttc', '/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc', '/System/Library/Fonts/STHeiti Light.ttc']
    path = next((p for p in candidates if p and Path(p).is_file()), None)
    if not path:
        raise ValueError('证书中文字体未配置，请联系管理员。')
    pdfmetrics.registerFont(TTFont('CertificateChinese', path, subfontIndex=0))
    return 'CertificateChinese'


def render(request):
    source = Path(request['sourcePath']).resolve(strict=True)
    output = Path(request['outputDir']).resolve(strict=True)
    if source.stat().st_size > 25 * 1024 * 1024:
        raise ValueError('模板文件不能超过 25 MB。')
    with tempfile.TemporaryDirectory(prefix='render-', dir=output) as directory:
        work = Path(directory)
        pages = load_pages(source, request['sourceName'], work)
        fields = request.get('fields') or []
        values = request.get('values') or {}
        font = setup_font() if fields else None
        target = output / 'certificate.pdf'
        pdf = canvas.Canvas(str(target), pageCompression=1)
        pdf.setTitle('个性化证书')
        for index, (width, height, background) in enumerate(pages):
            pdf.setPageSize((width, height))
            pdf.drawImage(str(background), 0, 0, width, height)
            for field in fields:
                if field['page'] != index:
                    continue
                text = field.get('text', '') if field['key'] == 'text' else values.get(field['key'], '')
                text = str(text).replace('\r', ' ').replace('\n', ' ')
                if not text:
                    raise ValueError('接收人的证书字段尚未填写，请补全资料后再生成。')
                left, top, box_width = field['x'] * width, field['y'] * height, field['width'] * width
                size = float(field['fontSize'])
                natural_width = pdfmetrics.stringWidth(text, font, size)
                if natural_width > box_width:
                    size *= box_width / natural_width
                if size < 6:
                    raise ValueError('字段内容过长，请加宽文本框或缩短内容。')
                ascent, descent = pdfmetrics.getAscentDescent(font, size)
                if top + ascent - descent > height:
                    raise ValueError('字段超出页面底部，请上移字段。')
                pdf.setFont(font, size)
                color = field.get('color', '#202a35').lstrip('#')
                pdf.setFillColorRGB(*(int(color[i:i + 2], 16) / 255 for i in (0, 2, 4)))
                baseline = height - top - ascent
                if field.get('align') == 'center':
                    pdf.drawCentredString(left + box_width / 2, baseline, text)
                elif field.get('align') == 'right':
                    pdf.drawRightString(left + box_width, baseline, text)
                else:
                    pdf.drawString(left, baseline, text)
            pdf.showPage()
        pdf.save()
        result = {'pdfPath': str(target), 'pages': [], 'previewPath': ''}
        for index, (width, height, _) in enumerate(pages):
            prefix = output / f'page-{index}'
            command(['pdftoppm', '-f', str(index + 1), '-l', str(index + 1), '-singlefile', '-scale-to', '1400', '-png', str(target), str(prefix)])
            result['pages'].append({'width': width, 'height': height, 'previewPath': str(prefix.with_suffix('.png'))})
        preview_page = request.get('previewPage', 0)
        if not 0 <= preview_page < len(pages):
            raise ValueError('预览页码超出模板页数。')
        result['previewPath'] = result['pages'][preview_page]['previewPath']
        return result


def main():
    resource.setrlimit(resource.RLIMIT_CPU, (90, 90))
    resource.setrlimit(resource.RLIMIT_FSIZE, (80 * 1024 * 1024, 80 * 1024 * 1024))
    resource.setrlimit(resource.RLIMIT_NOFILE, (256, 256))
    if sys.platform.startswith('linux'):
        resource.setrlimit(resource.RLIMIT_AS, (2 * 1024 ** 3, 2 * 1024 ** 3))
    os.umask(0o077)
    try:
        result = render(json.load(sys.stdin))
        json.dump(result, sys.stdout, ensure_ascii=False)
    except Exception as exc:
        message = str(exc) if isinstance(exc, ValueError) else '无法处理此模板，请检查文件或另存为 PDF 后重试。'
        json.dump({'error': message}, sys.stdout, ensure_ascii=False)
        sys.exit(1)


if __name__ == '__main__':
    main()
