#!/usr/bin/env python3
"""Compare a local official KVMD checkout with the checked-in API inventory.

Usage: python3 scripts/audit-api.py /path/to/kvmd [/path/to/ustreamer]
No upstream Python code is executed. Dynamic parameters are expanded explicitly
below from their defining classes and remote driver option schemas.
"""
import ast
import json
import hashlib
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]

def inventory(checkout):
    checkout = pathlib.Path(checkout)
    files = sorted((checkout / 'kvmd/apps/kvmd/api').rglob('*.py'))
    files += [checkout / 'kvmd/apps/kvmd/server.py', checkout / 'kvmd/apps/media/server.py']
    records = []
    for file in files:
        tree = ast.parse(file.read_text())
        functions = {n.name: n for n in ast.walk(tree) if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))}
        constants = {n.targets[0].attr: n.value.value for n in ast.walk(tree)
                     if isinstance(n, ast.Assign) and len(n.targets) == 1 and isinstance(n.targets[0], ast.Attribute)
                     and isinstance(n.value, ast.Constant) and isinstance(n.value.value, str)}
        # Class constants such as __T_VIDEO are simple Name assignments.
        constants.update({n.targets[0].id: n.value.value for n in ast.walk(tree)
                     if isinstance(n, ast.Assign) and len(n.targets) == 1 and isinstance(n.targets[0], ast.Name)
                     and isinstance(n.value, ast.Constant) and isinstance(n.value.value, str)})
        def parameters(fn, seen=None):
            seen = set() if seen is None else seen
            if fn.name in seen: return set()
            seen.add(fn.name)
            found = set()
            for n in ast.walk(fn):
                if isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute):
                    owner = ast.unparse(n.func.value)
                    if (owner in ('req.query', 'query') or owner == 'params' and fn.name == '__login_handler' or owner == 'await req.json()') and n.func.attr in ('get', 'pop') and n.args:
                        arg = n.args[0]
                        if isinstance(arg, ast.Constant) and isinstance(arg.value, str): found.add(arg.value)
                        elif isinstance(arg, ast.Attribute) and arg.attr in constants: found.add(constants[arg.attr])
                    if n.func.attr in functions and any(isinstance(a, ast.Name) and a.id == 'req' for a in n.args):
                        found |= parameters(functions[n.func.attr], seen)
                if isinstance(n, ast.Subscript) and ast.unparse(n.value) == 'req.query' and isinstance(n.slice, ast.Constant):
                    found.add(n.slice.value)
                # Parameter/validator dispatch tables used by HID, MSD, Switch, streamer.
                if isinstance(n, ast.List):
                    for item in n.elts:
                        if isinstance(item, ast.Tuple) and item.elts and isinstance(item.elts[0], ast.Constant) and isinstance(item.elts[0].value, str):
                            found.add(item.elts[0].value)
            return found
        for fn in functions.values():
            for d in fn.decorator_list:
                if not (isinstance(d, ast.Call) and isinstance(d.func, ast.Name) and d.func.id == 'exposed_http'): continue
                method, path = [ast.literal_eval(a) for a in d.args[:2]]
                media = 'apps/media' in str(file)
                if media: path = '/api/media' + ('' if path == '/' else path)
                elif not path.startswith('/redfish/'): path = '/api' + path
                path = path.replace('{sid}', '{id}')
                params = parameters(fn)
                location = 'query'
                if path == '/api/auth/login': location = 'form'
                if 'InsertMedia' in path or 'ComputerSystem.Reset' in path: location = 'json'
                if path == '/api/switch/set_colors':
                    types = ast.parse((checkout / 'kvmd/apps/kvmd/switch/types.py').read_text())
                    roles = next(n.value.args[0] for n in ast.walk(types) if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == 'ROLES' for t in n.targets))
                    params |= set(ast.literal_eval(roles))
                if path == '/api/msd/set_params':
                    for remote in (checkout / 'kvmd/nbd/remotes').glob('*.py'):
                        rt = ast.parse(remote.read_text())
                        for f in ast.walk(rt):
                            if isinstance(f, ast.FunctionDef) and f.name == 'get_options':
                                for node in ast.walk(f):
                                    if isinstance(node, ast.Dict): params |= {k.value for k in node.keys if isinstance(k, ast.Constant) and isinstance(k.value, str)}
                    params.discard('url') # image supplies the URL; the remaining options are forwarded.
                if 'InsertMedia' in path:
                    params.add('Oem.PiKVM.DriveOptical')
                params.discard('Oem')
                records.append(dict(method=method, path=path, location=location, parameters=sorted(params), source=str(file.relative_to(checkout))))
    return sorted(records, key=lambda r: (r['path'], r['method']))

def websocket_inventory(checkout):
    checkout=pathlib.Path(checkout)
    result=[]
    for path,route in [('kvmd/apps/kvmd/server.py','/api/ws'),('kvmd/apps/kvmd/api/hid.py','/api/ws'),('kvmd/apps/media/server.py','/api/media/ws')]:
        tree=ast.parse((checkout/path).read_text())
        functions={n.name:n for n in ast.walk(tree) if isinstance(n,(ast.FunctionDef,ast.AsyncFunctionDef))}
        def signature(fn,seen=None):
            seen=set() if seen is None else seen
            if fn.name in seen:return ''
            seen.add(fn.name)
            text=ast.dump(fn,include_attributes=False)
            for node in ast.walk(fn):
                if isinstance(node,ast.Call) and isinstance(node.func,ast.Attribute) and node.func.attr in functions:
                    text+=signature(functions[node.func.attr],seen)
            return text
        for fn in functions.values():
            for decorator in fn.decorator_list:
                if isinstance(decorator,ast.Call) and isinstance(decorator.func,ast.Name) and decorator.func.id=='exposed_ws':
                    selector=ast.literal_eval(decorator.args[0])
                    result.append(dict(path=route,selector=selector,source=path,handler_sha256=hashlib.sha256(signature(fn).encode()).hexdigest()))
    return sorted(result,key=lambda r:(r['path'],str(r['selector'])))

def video_inventory(checkout):
    import re
    text=(pathlib.Path(checkout)/'src/ustreamer/http/server.c').read_text()
    routes=set(re.findall(r'evhttp_set_cb\(run->http, "([^"\n]+)"',text))
    # Exclude the HTML page and favicon; retain the root's MJPG compatibility API.
    assert routes=={'/','/favicon.ico','/state','/snapshot','/stream'}, 'uStreamer HTTP routes changed'
    params=sorted(set(re.findall(r'^\s*PARSE_PARAM\(\w+, (\w+)\)',text,re.MULTILINE)))
    actions=sorted(set(re.findall(r'evhttp_find_header\(&params, "(\w+)"\)',text)))
    result=[]
    for suffix,query in [('',sorted(params+actions)),('/state',[]),('/snapshot',[]),('/stream',params)]:
        result.append(dict(method='GET',path='/streamer'+suffix,location='query',parameters=query,source='src/ustreamer/http/server.c'))
    return sorted(result,key=lambda r:r['path'])

if __name__ == '__main__':
    if len(sys.argv) not in (2,3): raise SystemExit(__doc__)
    records = inventory(sys.argv[1])
    expected = json.loads((ROOT / 'api/upstream.json').read_text())
    handlers=websocket_inventory(sys.argv[1])
    if handlers != expected['websocket_handlers']:
        print('WebSocket protocol handlers changed; review JSON fields and binary layouts before updating api/upstream.json')
        raise SystemExit(1)
    if records != expected['operations']:
        import difflib
        print('\n'.join(difflib.unified_diff(json.dumps(expected['operations'], indent=2).splitlines(), json.dumps(records, indent=2).splitlines(), fromfile='pinned inventory', tofile='upstream checkout')))
        raise SystemExit(1)
    sha = subprocess.check_output(['git', '-C', sys.argv[1], 'rev-parse', 'HEAD'], text=True).strip()
    print(f'PASS: {len(records)} HTTP/upgrade routes and all extracted parameters match KVMD {sha}')

    if len(sys.argv)==3:
        expected_video=json.loads((ROOT/'api/ustreamer.json').read_text())
        assert video_inventory(sys.argv[2])==expected_video['operations'], 'uStreamer API differs from pinned inventory'
        print('PASS: uStreamer compatibility/state/snapshot/MJPEG routes and parameters match')
