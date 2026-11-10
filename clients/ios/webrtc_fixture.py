"""Loopback-only WebRTC integration fixture; never a production phone or PIN."""
import asyncio
import json
import logging
import secrets
import math
import array
import os

from aiohttp import web
from aiortc import AudioStreamTrack, RTCConfiguration, RTCPeerConnection, RTCSessionDescription, VideoStreamTrack
from av import VideoFrame


class Pattern(VideoStreamTrack):
    async def recv(self):
        pts, time_base = await self.next_timestamp()
        light = os.environ.get('HALFCLOUD_FIXTURE_LIGHT') == '1'
        if light:
            await asyncio.sleep(.16)
        frame = VideoFrame(320 if light else 640, 180 if light else 360, "yuv420p")
        for index, plane in enumerate(frame.planes):
            value = (40 + int(pts / 3000) % 160) if index == 0 else (100 if index == 1 else 160)
            plane.update(bytes([value]) * plane.buffer_size)
        frame.pts, frame.time_base = pts, time_base
        return frame


class Tone(AudioStreamTrack):
    async def recv(self):
        frame = await super().recv()
        samples = array.array('h', (int(5000 * math.sin(2 * math.pi * 440 * ((frame.pts or 0) + i) / frame.sample_rate)) for i in range(frame.samples)))
        raw = samples.tobytes()
        frame.planes[0].update(raw + bytes(frame.planes[0].buffer_size - len(raw)))
        return frame

peers = set()
trust_states = {18741: {"requests": {}, "credentials": {}}, 18742: {"requests": {}, "credentials": {}}}
credential_sockets = {}

metrics = {"offers": 0, "connected": 0, "controlPackets": 0, "outboundAudioBytes": 0, "outboundVideoBytes": 0, "connectionsByDevice": {}, "approvalRequests": 0, "credentialSessions": 0, "terminalJobs":0,"toolReads":0,"networkDrops":0}


def selected(request):
    return request.headers.get("X-RemoteConsole-Device", "") if request.url.port == 18742 else "legacy"


def authorized(request):
    return request.headers.get("X-RemoteConsole-Controller", "") in trust_states[request.url.port]["credentials"]


async def trust_api(request):
    state = trust_states[request.url.port]
    path = request.path.removeprefix("/_remoteconsole/trust/")
    if path == "status":
        return web.json_response({"supported": True, "authenticated": authorized(request)})
    data = await request.json()
    if path == "session":
        if data.get("credential") not in state["credentials"]:
            return web.json_response({"error": "approval_required"}, status=401)
        metrics["credentialSessions"] += 1
        return web.json_response({"authenticated": True})
    if path == "requests":
        ident, secret = secrets.token_urlsafe(24), secrets.token_urlsafe(32)
        controller, credential = secrets.token_urlsafe(16), secrets.token_urlsafe(32)
        state["requests"][ident] = {"secret": secret, "credential": controller + "." + credential, "name": data.get("name", "Fixture iPhone")}
        metrics["approvalRequests"] += 1
        return web.json_response({"request_id": ident, "request_secret": secret, "approval_code": "TEST-2345", "interval": 2}, status=201)
    if path in ["poll", "cancel"]:
        record = state["requests"].get(data.get("request_id"))
        if not record or data.get("request_secret") != record["secret"]:
            return web.json_response({"state": "request_expired"}, status=410)
        if path == "cancel":
            del state["requests"][data["request_id"]]
            return web.json_response({"ok": True})
        # This loopback-only fixture substitutes an owner approval. Real phone
        # approval and revocation are exercised separately against the gateway.
        state["credentials"][record["credential"]] = True
        return web.json_response({"state": "approved", "credential": record["credential"], "controller": {"name": record["name"]}})
    raise web.HTTPNotFound()


async def api(request):
    if request.path.startswith("/_remoteconsole/trust/"):
        return await trust_api(request)
    if request.path == "/fixture/revoke" and request.method == "POST":
        credentials = list(trust_states[request.url.port]["credentials"])
        trust_states[request.url.port]["credentials"].clear()
        for credential in credentials:
            for ws in list(credential_sockets.get(credential, set())):
                await ws.close(code=1008, message=b"controller_revoked")
        return web.json_response({"revoked": True})
    if request.path == "/fixture/drop" and request.method == "POST":
        metrics["networkDrops"]+=1
        metrics["approvalRequestsAtDrop"]=metrics["approvalRequests"]
        metrics["credentialSessionsAtDrop"]=metrics["credentialSessions"]
        for sockets in list(credential_sockets.values()):
            for ws in list(sockets): await ws.close(code=1001,message=b"network fixture")
        return web.json_response({"dropped":True})
    if request.path == "/health":
        return web.json_response({"ok": True})
    if request.path == "/metrics":
        return web.json_response(metrics)
    if request.path == "/_remoteconsole/devices":
        if request.url.port != 18742:
            raise web.HTTPNotFound()
        return web.json_response({"registry_version": 1, "devices": [{"id": "fixture-a", "name": "測試電話 A", "online": True}, {"id": "fixture-b", "name": "測試電話 B", "online": True}]}, headers={"X-RemoteConsole-Registry": "1"})
    device_id = selected(request)
    if request.path == "/api/unlock":
        data = await request.json()
        accepted = device_id in ["legacy", "fixture-a", "fixture-b"] and data.get("pin") == ("654321" if device_id == "fixture-b" else "123456")
        response = web.json_response({"result": "success" if accepted else "failed", "leftTries": 4})
        if accepted:
            response.set_cookie("fixture", "local-only-" + device_id, httponly=True)
        return response
    if not authorized(request) and request.cookies.get("fixture") != "local-only-" + device_id:
        return web.json_response({"error": "fixture authentication required"}, status=401)
    if request.path == "/api/tools/files":
        metrics['toolReads']+=1
        return web.json_response({"path":".","entries":[{"name":"Download","path":"Download","directory":True,"size":0}],"next_offset":-1})
    if request.path == "/api/tools/status":
        return web.json_response({"version":"fixture","root":True,"device":{"model":"Synthetic fixture","android":"test","battery_percent":73,"temperature_c":28.5,"network":"loopback"}})
    if request.path == "/_remoteconsole/relays/status":
        return web.json_response({"relays":[{"id":"hk","name":"香港測試中繼","available":True,"report":{"day":{"rx":1000,"tx":2000},"month":{"rx":1000,"tx":2000},"total":{"rx":1000,"tx":2000}}}]})
    if request.path == "/api/tools/terminal/jobs" and request.method == "POST":
        metrics['terminalJobs']+=1
        return web.json_response({"id":"fixture-job"},status=202)
    if request.path == "/api/tools/terminal/jobs/fixture-job":
        return web.json_response({"output":"halfcloud-test\n[synthetic fixture]\n","offset":35,"done":True,"exit_code":0})
    if request.path == "/api/device/list":
        return web.json_response({"devices": [{"device_type": "android", "device_id": "loopback-" + device_id, "name": "WebRTC 測試串流（非真機）", "ip": "127.0.0.1", "port": 0}]})
    if request.path == "/api/screen/sessions":
        return web.json_response({"stream_options": {}})
    if request.path == "/api/ice-config":
        return web.json_response({"iceServers": []})
    raise web.HTTPNotFound()


async def socket(request):
    device_id = selected(request)
    if device_id not in ["legacy", "fixture-a", "fixture-b"] or (not authorized(request) and request.cookies.get("fixture") != "local-only-" + device_id):
        raise web.HTTPUnauthorized()
    ws = web.WebSocketResponse()
    await ws.prepare(request)
    credential = request.headers.get("X-RemoteConsole-Controller", "")
    credential_sockets.setdefault(credential, set()).add(ws)
    pc = RTCPeerConnection(RTCConfiguration(iceServers=[]))
    peers.add(pc)

    @pc.on("connectionstatechange")
    async def state():
        if pc.connectionState == "connected":
            metrics["connected"] += 1
            counts = metrics["connectionsByDevice"]
            counts[device_id] = counts.get(device_id, 0) + 1
        print(json.dumps({"connectionState": pc.connectionState}), flush=True)

    @pc.on("datachannel")
    def channel_opened(channel):
        @channel.on("message")
        def message(data):
            if isinstance(data, bytes):
                metrics["controlPackets"] += 1

    @pc.on("iceconnectionstatechange")
    async def ice_state():
        print(json.dumps({"device": device_id, "iceState": pc.iceConnectionState}), flush=True)

    try:
        offer = await ws.receive_json()
        print(json.dumps({"device":device_id,"offerCandidates":[line for line in offer["sdp"].splitlines() if line.startswith(("a=candidate:","a=ice-options:"))]}),flush=True)
        metrics["offers"] += 1
        await pc.setRemoteDescription(RTCSessionDescription(sdp=offer["sdp"], type="offer"))
        pc.addTrack(Pattern())
        pc.addTrack(Tone())
        await pc.setLocalDescription(await pc.createAnswer())
        print(json.dumps({"device":device_id,"answerCandidates":[line for line in pc.localDescription.sdp.splitlines() if line.startswith("a=candidate:")]}),flush=True)
        await ws.send_json({"stage": "webrtc_init", "sdp": pc.localDescription.sdp})
        await ws.send_json({"stage": "webrtc_metainfo", "capabilities": {"can_control": True, "can_clipboard": True}, "media_meta": {"width": 640, "height": 360}})
        async for _ in ws:
            pass
    finally:
        stats = await pc.getStats()
        for stat in stats.values():
            if stat.type == "outbound-rtp":
                key = "outboundAudioBytes" if stat.kind == "audio" else "outboundVideoBytes"
                metrics[key] += stat.bytesSent
        await pc.close()
        peers.discard(pc)
        credential_sockets.get(credential, set()).discard(ws)
        print(json.dumps(metrics), flush=True)
    return ws


async def cleanup(app):
    await asyncio.gather(*(pc.close() for pc in list(peers)))


def application():
    app = web.Application()
    app.router.add_get("/screen/ws", socket)
    app.router.add_route("*", "/{path:.*}", api)
    app.on_shutdown.append(cleanup)
    return app


app = application()


async def main():
    runner = web.AppRunner(app)
    await runner.setup()
    await web.TCPSite(runner, "127.0.0.1", 18741).start()
    await web.TCPSite(runner, "127.0.0.1", 18742).start()
    try:
        await asyncio.Event().wait()
    finally:
        await runner.cleanup()


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    asyncio.run(main())
