import yaml, json, sys
from pathlib import Path

# Example responses for every route in the spec, for the stubbed browser walk
# (see README.md). Usage: gen-stubs.py <out.json>
spec = yaml.safe_load(open(Path(__file__).resolve().parents[2] / 'api' / 'openapi.yaml'))
comps = spec.get('components', {}).get('schemas', {})

def deref(s):
    while isinstance(s, dict) and '$ref' in s:
        s = comps[s['$ref'].split('/')[-1]]
    return s

def example(s, depth=0):
    s = deref(s)
    if not isinstance(s, dict) or depth > 7:
        return None
    if 'example' in s: return s['example']
    if 'default' in s: return s['default']
    if 'enum' in s: return s['enum'][0]
    if 'allOf' in s:
        out = {}
        for part in s['allOf']:
            v = example(part, depth + 1)
            if isinstance(v, dict): out.update(v)
        return out
    if 'oneOf' in s or 'anyOf' in s:
        return example((s.get('oneOf') or s.get('anyOf'))[0], depth + 1)
    t = s.get('type')
    if isinstance(t, list):
        t = next((x for x in t if x != 'null'), None)
    if t is None and 'properties' in s: t = 'object'
    if t == 'object':
        return {k: example(v, depth + 1) for k, v in (s.get('properties') or {}).items()}
    if t == 'array': return []
    if t == 'string':
        f = s.get('format')
        if f == 'date-time': return '2026-09-05T12:00:00Z'
        if f == 'uuid': return '00000000-0000-4000-8000-000000000001'
        return ''
    if t in ('integer', 'number'): return 0
    if t == 'boolean': return False
    return None

stubs = {}
for path, item in spec['paths'].items():
    for method, op in item.items():
        if method.lower() not in ('get', 'post', 'put', 'patch', 'delete'): continue
        resps = op.get('responses', {})
        code = next((c for c in ('200', '201', '202') if c in resps), None)
        body = None
        if code:
            js = (resps[code].get('content') or {}).get('application/json')
            if js and 'schema' in js: body = example(js['schema'])
        stubs[f"{method.upper()} {path}"] = {'status': int(code) if code else 204, 'body': body}
json.dump(stubs, open(sys.argv[1], 'w'), indent=1)
print(len(stubs), 'stubs')
