import base64
import hashlib
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch
import relay


PROFILE = dict(schema=1, kind='halfcloud-relay', public_ip='93.184.216.34', port=3478, secret='synthetic-'+'x'*48)


class RelayTests(unittest.TestCase):
    def test_link_round_trip_and_reject_invalid_profiles(self):
        self.assertEqual(relay.parse_link(relay.link_code(PROFILE)), PROFILE)
        for change in [dict(public_ip='127.0.0.1'), dict(public_ip='169.254.169.254'), dict(port=0), dict(port=True), dict(secret='short'), dict(secret='x'*48+'\nno-auth')]:
            with self.assertRaises(ValueError): relay.validate_profile(dict(PROFILE, **change))
        with self.assertRaises(ValueError): relay.parse_link('bad code')

    def test_turn_config_uses_nat_mapping_and_bounds(self):
        text = relay.turn_configuration(dict(PROFILE, relay_ip='10.20.30.40'))
        self.assertIn('external-ip=93.184.216.34/10.20.30.40', text)
        self.assertIn('min-port=49160', text)
        self.assertIn('use-auth-secret', text)
        self.assertNotIn('\nno-auth\n', text)


class GatewayTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        base = Path(self.temp.name)
        data = base/'private'; data.mkdir()
        self.data = data
        self.old = json.dumps({'stun_urls':['stun:old.example.com:3478'], 'turn_urls':['turn:old.example.com:3478'], 'turn_shared_secret':'old-'+'a'*48}).encode()
        registry = {'version':1,'default_device':'existing-first','devices':[
            {'id':'existing-first','private':'/private'}, {'id':'second','private':'/private/devices/second'}]}
        relay.write_json(data/'devices.json', registry)
        for parent in [data, data/'devices/second']:
            relay.atomic(parent/'ice.private.json', self.old)
            relay.atomic(parent/'phone-frpc.toml', b'original-tunnel-token-and-path')
            relay.atomic(parent/'enrollment.pub', b'original-phone-public-key')
        relay.atomic(data/'controller-trust/controllers.json', b'{"approved":"keep-original","revoked":"remain-revoked"}')
        self.calls = []
        self.good = True
        self.manager = types.SimpleNamespace(p=types.SimpleNamespace(data=data,backups=base/'backups'),
            config=lambda: {'domain':'control.example.com'}, compose=lambda *args:self.calls.append(args), healthy=self.healthy)
        self.probes = []

    def healthy(self):
        if self.good:return True
        self.good=True
        return False

    def others(self):
        return {str(p.relative_to(self.data)): p.read_bytes() for p in self.data.rglob('*') if p.is_file() and p.name not in ['ice.private.json','relay-link-state.json','relay-nodes.json']}

    def connect(self, **kwargs):
        relay.connect_gateway(self.manager, PROFILE, check=lambda p:self.probes.append(p), **kwargs)

    def test_connect_every_phone_idempotent_preserves_all_authorizations_and_rollback(self):
        before=self.others()
        self.connect()
        self.assertEqual(self.others(), before)
        for path in relay.profile_paths(self.data):
            self.assertIn('turn:93.184.216.34:3478?transport=udp', json.loads(path.read_bytes())['turn_urls'])
        regions=json.loads((self.data/'ice.private.json').read_bytes())['relay_regions']
        self.assertEqual([r['id'] for r in regions],['hk','jp'])
        self.assertNotEqual(regions[0]['turn_shared_secret'],regions[1]['turn_shared_secret'])
        self.assertEqual(self.calls,[('up','-d','--force-recreate','gateway')])
        self.connect()
        self.assertEqual(len(self.calls),1)
        relay.connect_gateway(self.manager, restore=True)
        self.assertEqual(self.others(),before)
        for path in relay.profile_paths(self.data):self.assertEqual(path.read_bytes(),self.old)

    def test_failed_gateway_recreation_rolls_back_only_ice(self):
        before=self.others();self.good=False
        with self.assertRaises(RuntimeError):self.connect()
        self.assertEqual(self.others(),before)
        for path in relay.profile_paths(self.data):self.assertEqual(path.read_bytes(),self.old)
        self.assertFalse((self.data/'relay-link-state.json').exists())

    def test_unreachable_or_wrong_secret_relay_changes_nothing(self):
        before={str(p):p.read_bytes() for p in self.data.rglob('*') if p.is_file()}
        with self.assertRaises(OSError):
            relay.connect_gateway(self.manager, PROFILE, check=lambda p:(_ for _ in ()).throw(OSError('unreachable')))
        self.assertEqual(before,{str(p):p.read_bytes() for p in self.data.rglob('*') if p.is_file()})
        self.assertEqual(self.calls,[])

    def test_new_phone_after_connect_is_also_restored(self):
        self.connect()
        registry=json.loads((self.data/'devices.json').read_text())
        registry['devices'].append({'id':'third','private':'/private/devices/third'})
        relay.write_json(self.data/'devices.json',registry)
        relay.atomic(self.data/'devices/third/ice.private.json',(self.data/'ice.private.json').read_bytes())
        relay.connect_gateway(self.manager,restore=True)
        self.assertEqual((self.data/'devices/third/ice.private.json').read_bytes(),self.old)

    def test_path_escape_and_drift_fail_closed(self):
        registry=json.loads((self.data/'devices.json').read_text())
        registry['devices'][1]['private']='/private/../../other'
        relay.write_json(self.data/'devices.json',registry)
        with self.assertRaises(ValueError):self.connect()
        self.assertEqual(self.calls,[])

    def test_disconnect_refuses_to_overwrite_a_later_network_edit(self):
        self.connect()
        later=json.dumps({'turn_urls':['turn:later.example.com:3478']}).encode()
        relay.atomic(self.data/'ice.private.json',later)
        with self.assertRaises(ValueError):relay.connect_gateway(self.manager,restore=True)
        self.assertEqual((self.data/'ice.private.json').read_bytes(),later)


if __name__=='__main__':unittest.main()
