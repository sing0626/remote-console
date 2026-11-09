import hashlib,json,subprocess,tempfile,unittest
from pathlib import Path
from initialize import initialize
from register_device import register
class DeploymentTests(unittest.TestCase):
 def test_fresh_identity_and_registration_are_runtime_only(self):
  with tempfile.TemporaryDirectory() as temp:
   root=Path(temp)
   def key(name):
    private=root/(name+'.key');public=root/(name+'.pub')
    subprocess.run(['openssl','genpkey','-algorithm','RSA','-pkeyopt','rsa_keygen_bits:3072','-out',str(private)],check=True,capture_output=True)
    subprocess.run(['openssl','pkey','-in',str(private),'-pubout','-out',str(public)],check=True,capture_output=True);return public
   first,second=key('one'),key('two');base=root/'private';compose=root/'compose.devices.generated.json'
   initialize('control.example.com','93.184.216.34',first,base,compose)
   registry=json.loads((base/'devices.json').read_text());self.assertEqual(registry['default_device'],'primary')
   before={str(p.relative_to(base)):hashlib.sha256(p.read_bytes()).hexdigest() for p in base.rglob('*') if p.is_file()}
   with self.assertRaises(ValueError):initialize('control.example.com','93.184.216.34',second,base,compose)
   self.assertEqual(before,{str(p.relative_to(base)):hashlib.sha256(p.read_bytes()).hexdigest() for p in base.rglob('*') if p.is_file()})
   result=register(base,base/'enrollment.pub','secondary','Android phone B',second,compose,False)
   self.assertEqual(result['public_tcp_port'],7001)
   profile=(base/'devices/secondary/phone-frpc.toml').read_bytes()
   register(base,base/'enrollment.pub','secondary','Android phone B',second,compose,False)
   self.assertEqual(profile,(base/'devices/secondary/phone-frpc.toml').read_bytes())
   self.assertEqual([d['id'] for d in json.loads((base/'devices.json').read_text())['devices']],['primary','secondary'])
   self.assertEqual(list(json.loads(compose.read_text())['services']),['signaling-secondary'])
if __name__=='__main__':unittest.main()
