#!/usr/bin/env python3
"""Generate a Homebrew binary formula from verified release SHA256SUMS."""
import argparse
import importlib.util
from pathlib import Path
import re

SPEC=importlib.util.spec_from_file_location('release',Path(__file__).with_name('build-release.py'))
RELEASE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(RELEASE)

def formula(version,checksums):
    RELEASE.validate_version(version)
    sums={}
    for line in checksums.splitlines():
        if not line.strip():continue
        fields=line.split()
        if len(fields)!=2 or not re.fullmatch('[a-f0-9]{64}',fields[0]) or fields[1] in sums:raise ValueError('invalid or duplicate checksum record')
        sums[fields[1]]=fields[0]
    rows=['class PikvmCli < Formula','  desc "Control PiKVM from terminals and automation"','  homepage "https://github.com/infoe-labs/pikvm-cli"',f'  version "{version[1:]}"','']
    for system,block in [('darwin','macos'),('linux','linux')]:
        rows+=['  on_'+block+' do','    if Hardware::CPU.arm?']
        for index,arch in enumerate(('arm64','amd64')):
            name=RELEASE.archive_name(version,system,arch)
            if name not in sums:raise ValueError('missing required Homebrew archive checksum')
            if index:rows+=['    else']
            rows += [f'      url "https://github.com/infoe-labs/pikvm-cli/releases/download/{version}/{name}"',f'      sha256 "{sums[name]}"']
        rows+=['    end','  end','']
    rows+=['  def install','    bin.install "pikvm-cli"','  end','','  test do','    assert_match version.to_s, shell_output("#{bin}/pikvm-cli --version")','  end','end','']
    return '\n'.join(rows)
if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--version',required=True);parser.add_argument('--checksums',type=Path,required=True);parser.add_argument('--output',type=Path,required=True);args=parser.parse_args()
    try:content=formula(args.version,args.checksums.read_text())
    except (ValueError,OSError):parser.exit(1,'cannot generate formula from release checksums\n')
    args.output.parent.mkdir(parents=True,exist_ok=True);args.output.write_text(content)
