#!/usr/bin/env python3
"""Build reproducible PiKVM CLI release archives and SHA256SUMS."""
import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = (('linux','amd64',''),('linux','arm64',''),('linux','arm','7'),('darwin','amd64',''),('darwin','arm64',''),('windows','amd64',''),('windows','arm64',''))
def validate_version(value):
    if not re.fullmatch(r'v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?',value):
        raise ValueError('version must be a v-prefixed semantic version')
    if '-' in value:
        for part in value.split('-',1)[1].split('.'):
            if part.isdigit() and len(part)>1 and part.startswith('0'): raise ValueError('numeric prerelease identifiers cannot have leading zeros')
    return value

def archive_name(version,system,arch,arm=''):
    return f'pikvm-cli_{version}_{system}_{arch+"v"+arm if arm else arch}'+('.zip' if system=='windows' else '.tar.gz')

def pack(destination,name,data):
    if destination.suffix=='.zip':
        with zipfile.ZipFile(destination,'w',compression=zipfile.ZIP_DEFLATED) as archive:
            member=zipfile.ZipInfo(name,date_time=(1980,1,1,0,0,0));member.external_attr=0o755<<16;archive.writestr(member,data,compress_type=zipfile.ZIP_DEFLATED)
    else:
        with destination.open('wb') as stream, gzip.GzipFile(filename='',mode='wb',fileobj=stream,mtime=0) as compressed, tarfile.open(fileobj=compressed,mode='w') as archive:
            member=tarfile.TarInfo(name);member.size=len(data);member.mode=0o755;member.mtime=0;archive.addfile(member,io.BytesIO(data))

def build(version,destination,targets=TARGETS):
    validate_version(version);destination.mkdir(parents=True,exist_ok=True)
    archives=[]
    for system,arch,arm in targets:
        env=os.environ.copy();env.update(GOOS=system,GOARCH=arch,CGO_ENABLED='0');env.pop('GOARM',None)
        if arm:env['GOARM']=arm
        name='pikvm-cli.exe' if system=='windows' else 'pikvm-cli'
        executable=destination/name
        subprocess.run(['go','build','-trimpath','-buildvcs=false','-ldflags',f'-s -w -X main.version={version}','-o',str(executable),'./cmd/pikvm-cli'],cwd=ROOT,env=env,check=True)
        archive=destination/archive_name(version,system,arch,arm);pack(archive,name,executable.read_bytes());executable.unlink();archives.append(archive)
    sums=''.join(hashlib.sha256(path.read_bytes()).hexdigest()+'  '+path.name+'\n' for path in sorted(archives))
    (destination/'SHA256SUMS').write_text(sums)
    return archives

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--version',required=True);parser.add_argument('--output',type=Path,default=ROOT/'dist');args=parser.parse_args()
    try:build(args.version,args.output.resolve())
    except ValueError as exc:parser.error(str(exc))
