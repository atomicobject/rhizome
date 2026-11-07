"""Dependency evidence fails before a developer toolchain can mask missing runtime libraries."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from desktop_candidates import CandidateError
from desktop_candidates_test import VERSION, dependency_fixture
from desktop_dependencies import inspect_dependencies


class DependencyTests(unittest.TestCase):
    def setUp(self):
        temporary=tempfile.TemporaryDirectory(); self.addCleanup(temporary.cleanup)
        self.root=Path(temporary.name); self.cli=self.root/'rzm'; self.cli.write_bytes(b'synthetic canonical CLI')
        self.calls=[]
        self.needed='0x1 (NEEDED) Shared library: [libc.so.6]'
        self.symbols='Name: GLIBC_2.17\nName: GLIBC_2.2.5'
        self.listing='libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6 (0x1234)'
        self.interpreter='[Requesting program interpreter: /lib64/ld-linux-x86-64.so.2]'
        self.windows=dependency_fixture('windows-x86_64')
        self.environment={'PATH':'/developer/bin','LD_LIBRARY_PATH':'/developer/lib',
                          'LD_PRELOAD':'/developer/inject.so','GLIBC_TUNABLES':'synthetic'}

    def runner(self, command, **kwargs):
        self.calls.append((command,kwargs))
        output=''
        if command[0]=='cat': output='ID=ubuntu\nVERSION_ID="24.04"\n'
        elif command[0]=='getconf': output='glibc 2.39'
        elif command[:2]==['readelf','-d']: output=self.needed
        elif command[:2]==['readelf','--version-info']: output=self.symbols
        elif command[:2]==['readelf','-l']: output=self.interpreter
        elif command[0]=='ldd': output=self.listing
        elif command[0]=='objdump': output='DLL Name: KERNEL32.dll\n'
        elif command[0]=='powershell.exe': output=json.dumps(self.windows)
        elif command[-1]=='--version':
            self.assertEqual(self.cli.read_bytes(),Path(command[0]).read_bytes())
            self.assertNotEqual(self.cli.parent,kwargs['cwd'])
            self.assertEqual('/usr/bin:/bin',kwargs['env']['PATH'])
            output='rzm version v'+VERSION
        else: raise AssertionError(command)
        return subprocess.CompletedProcess(command,0,output,'')

    def inspect(self,target='linux-x86_64'):
        return inspect_dependencies(self.cli,target,VERSION,root=self.root,env=self.environment,runner=self.runner)

    def test_linux_records_real_imports_symbol_floor_and_runs_independent_copy(self):
        result=self.inspect()
        self.assertEqual('ubuntu-24.04-x86_64',result['baseline'])
        self.assertEqual('glibc 2.39',result['host']['libc'])
        self.assertEqual('2.17',result['minimumGlibc'])
        self.assertEqual('libc.so.6',result['imports'][0]['name'])
        for _,call in self.calls:
            self.assertNotIn('LD_LIBRARY_PATH',call['env']); self.assertNotIn('LD_PRELOAD',call['env'])
            self.assertNotIn('GLIBC_TUNABLES',call['env'])

    def test_linux_arm64_records_its_own_baseline_and_architecture(self):
        # aarch64 glibc binaries also list the loader as NEEDED; ldd names it by path only.
        self.needed+='\n0x1 (NEEDED) Shared library: [ld-linux-aarch64.so.1]'
        self.listing='libc.so.6 => /lib/aarch64-linux-gnu/libc.so.6 (0x1234)\n\t/lib/ld-linux-aarch64.so.1 (0x5678)'
        self.interpreter='[Requesting program interpreter: /lib/ld-linux-aarch64.so.1]'
        result=self.inspect('linux-aarch64')
        self.assertEqual('ubuntu-24.04-aarch64',result['baseline'])
        self.assertEqual('aarch64',result['host']['architecture'])
        self.assertEqual('/lib/ld-linux-aarch64.so.1',result['interpreter'])

    def test_linux_rejects_missing_toolchain_local_library_newer_glibc_and_local_interpreter(self):
        for attribute,value,message in (
            ('listing','libc.so.6 => not found','unresolved'),
            ('listing','libc.so.6 => /opt/toolchain/libc.so.6 (0x1234)','outside'),
            ('symbols','Name: GLIBC_2.40','newer'),
            ('needed',self.needed+'\n0x1 (RUNPATH) Library runpath: [$ORIGIN]','RPATH'),
            ('interpreter','[Requesting program interpreter: /tmp/.mount_app/ld.so]','interpreter')):
            original=getattr(self,attribute)
            with self.subTest(attribute=attribute),self.assertRaisesRegex(CandidateError,message):
                setattr(self,attribute,value); self.inspect()
            setattr(self,attribute,original)
        self.assertFalse(any(command[-1]=='--version' for command,_ in self.calls))

    def test_windows_records_system_dlls_and_rejects_imports_found_only_on_gcc_path(self):
        self.assertEqual('windows-11-x86_64',self.inspect('windows-x86_64')['baseline'])
        original=copy.deepcopy(self.windows)
        for patch in ({'path':'C:\\mingw64\\bin\\libgcc_s_seh-1.dll'}, {'signature':'unsigned'}):
            self.windows=copy.deepcopy(original); self.windows['imports'][0].update(patch)
            with self.subTest(patch=patch),self.assertRaises(CandidateError):
                self.inspect('windows-x86_64')
        self.windows=copy.deepcopy(original); self.windows['imports']=[]
        with self.assertRaisesRegex(CandidateError,'incomplete'):
            self.inspect('windows-x86_64')

    def test_native_tool_failure_is_a_failed_candidate_not_accepted_empty_evidence(self):
        def missing(*args,**kwargs): raise FileNotFoundError('missing readelf')
        with self.assertRaisesRegex(CandidateError,'inspection failed'):
            inspect_dependencies(self.cli,'linux-x86_64',VERSION,root=self.root,env={},runner=missing)


if __name__=='__main__': unittest.main()
