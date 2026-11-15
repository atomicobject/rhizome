#!/usr/bin/env python3
"""Serve verified test updates on loopback. Keep this terminal running."""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from urllib.parse import urlparse
from build import ROOT, MANIFEST
from check import all_artifacts


def feed(label):
    row=MANIFEST['builds'][label]
    signature=(ROOT/'artifacts'/label/'Rhizome-update-proof.app.tar.gz.sig').read_text().strip()
    return {'version':row['version'],'notes':'Isolated updater acceptance build '+label,'pub_date':'2026-11-15T00:00:00Z','platforms':{'darwin-aarch64':{'signature':signature,'url':f'http://127.0.0.1:19452/artifacts/{label}/Rhizome-update-proof.app.tar.gz'}}}


def select(label):
    assert label in 'ABC' and len(label)==1
    all_artifacts()
    file=ROOT/'feed/selected.json'
    file.parent.mkdir(exist_ok=True)
    temp=file.with_suffix('.tmp')
    temp.write_text(json.dumps(feed(label),indent=2)+'\n')
    temp.replace(file)
    print('Feed now offers '+label,flush=True)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path=urlparse(self.path).path
        if path=='/latest.json':
            data=(ROOT/'feed/selected.json').read_bytes(); mime='application/json'
        elif path in [f'/artifacts/{x}/Rhizome-update-proof.app.tar.gz' for x in 'ABC']:
            data=(ROOT/path.lstrip('/')).read_bytes(); mime='application/gzip'
        else:
            self.send_error(404); return
        self.send_response(200)
        self.send_header('Content-Type',mime)
        self.send_header('Content-Length',str(len(data)))
        self.send_header('Cache-Control','no-store')
        self.end_headers(); self.wfile.write(data)

    def log_message(self,fmt,*args):
        print(fmt%args,flush=True)


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('command',choices=['serve','select','check'])
    p.add_argument('label',nargs='?',choices=['A','B','C'],default='A')
    a=p.parse_args()
    if a.command=='select': select(a.label)
    elif a.command=='serve':
        all_artifacts()
        assert (ROOT/'feed/selected.json').exists(),'Select a build before serving'
        print('Serving verified artifacts at '+MANIFEST['endpoint'],flush=True)
        ThreadingHTTPServer(('127.0.0.1',19452),Handler).serve_forever()
    else:
        from urllib.request import urlopen
        remote=json.load(urlopen(MANIFEST['endpoint'],timeout=10))
        assert remote==feed(a.label)
        package=urlopen(remote['platforms']['darwin-aarch64']['url'],timeout=10).read()
        assert package==(ROOT/'artifacts'/a.label/'Rhizome-update-proof.app.tar.gz').read_bytes()
        print('Live feed and downloaded bytes match build '+a.label)
