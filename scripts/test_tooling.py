import importlib.util
import hashlib
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
import zipfile

def load(name):
    spec=importlib.util.spec_from_file_location(name,Path(__file__).with_name(name+'.py'));module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);return module
release=load('build-release');brew=load('homebrew-formula');coverage=load('check-coverage')
class ReleaseTests(unittest.TestCase):
    def test_versions_reject_injection_and_non_semver(self):
        for value in ['v0.1.0','v1.2.3-rc.1']:self.assertEqual(release.validate_version(value),value)
        for value in ['0.1.0','v01.0.0','v1.0','v1.2.3;command','v1.2.3-rc.01','../v1.2.3','v1.2.3\n']:
            with self.assertRaises(ValueError):release.validate_version(value)
    def test_archives_are_reproducible_and_executable(self):
        with tempfile.TemporaryDirectory() as directory:
            for suffix in ['tar.gz','zip']:
                a,b=[Path(directory)/f'{name}.{suffix}' for name in ['first','second']]
                release.pack(a,'pikvm-cli',b'synthetic binary');release.pack(b,'pikvm-cli',b'synthetic binary');self.assertEqual(a.read_bytes(),b.read_bytes())
                if suffix=='zip':
                    with zipfile.ZipFile(a) as archive:self.assertEqual(archive.read('pikvm-cli'),b'synthetic binary')
                else:
                    with tarfile.open(a) as archive:self.assertEqual(archive.getmember('pikvm-cli').mode,0o755);self.assertEqual(archive.extractfile('pikvm-cli').read(),b'synthetic binary')
    def test_formula_has_every_supported_platform_and_checksum(self):
        sums='\n'.join('a'*64+'  '+release.archive_name('v0.1.0',system,arch) for system in ['darwin','linux'] for arch in ['amd64','arm64'])
        text=brew.formula('v0.1.0',sums);self.assertEqual(text.count('sha256'),4);self.assertIn('version "0.1.0"',text);self.assertIn('--version',text)
        for bad in ['',sums+'\n'+sums,'invalid checksum']:
            with self.assertRaises(ValueError):brew.formula('v0.1.0',bad)
class CoverageTests(unittest.TestCase):
    def profile(self,lib_hits=1):
        return f'mode: atomic\n{coverage.MODULE}mcpserver/server.go:1.1,2.2 1 1\n{coverage.MODULE}cli/root.go:1.1,2.2 2 1\n{coverage.MODULE}lib/pikvm/client.go:1.1,2.2 3 {lib_hits}\n'
    def test_weighted_and_duplicate_blocks(self):
        text=self.profile()+f'{coverage.MODULE}cli/root.go:1.1,2.2 2 0\n{coverage.MODULE}lib/pikvm/services.go:1.1,2.2 7 1\n'
        scores=coverage.measure(text);self.assertEqual(scores['lib/pikvm','all'],100);self.assertEqual(scores['cli','all'],100)
        scores=coverage.measure(self.profile(0));self.assertEqual(scores['lib/pikvm','handwritten'],0)
    def test_missing_and_malformed_profiles_fail_closed(self):
        for text in ['', 'mode: invalid','mode: atomic\ninvalid',self.profile().replace('3 1','-1 1'),'mode: atomic']:
            with self.assertRaises(ValueError):coverage.measure(text)
if __name__=='__main__':unittest.main()
