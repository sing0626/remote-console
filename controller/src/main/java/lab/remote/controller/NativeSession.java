package lab.remote.controller;

import android.content.Context;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import org.json.*;
import org.webrtc.*;
import okhttp3.*;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.*;

/** Native WebRTC receiver. No browser, HTML, JavaScript or capture permission. */
public final class NativeSession {
    public interface Listener {
        void state(String label, boolean ready);
        void device(String name);
        void stats(String label);
        void dimensions(int width,int height);
        void message(String message);
        void clipboard(String value);
        default void approval(String name,String code) {}
    }
    private static EglBase egl;
    private static PeerConnectionFactory factory;
    public static synchronized EglBase.Context eglContext(Context context) {
        if(factory==null){
            PeerConnectionFactory.initialize(PeerConnectionFactory.InitializationOptions.builder(context.getApplicationContext()).createInitializationOptions());
            egl=EglBase.create();
            factory=PeerConnectionFactory.builder().setVideoDecoderFactory(new DefaultVideoDecoderFactory(egl.getEglBaseContext()))
                .setVideoEncoderFactory(new DefaultVideoEncoderFactory(egl.getEglBaseContext(),true,true)).createPeerConnectionFactory();
        }
        return egl.getEglBaseContext();
    }
    private final ExecutorService queue=Executors.newSingleThreadExecutor();
    private final Handler main=new Handler(Looper.getMainLooper());
    private final Listener listener;
    private final VideoSink renderer;
    private final OkHttpClient http;
    private final String origin;
    private final String routingDevice;
    private final ControllerCredentialStore credentialStore;
    private volatile String controllerCredential;
    private volatile boolean cancelling;
    private volatile String approvalRequest,approvalSecret;
    private final String authorizationUser,authorizationPassword;
    public static final class Host {
        public final String id,name; public final boolean online;
        Host(String id,String name,boolean online){this.id=id;this.name=name;this.online=online;}
    }
    /** null is a legacy single-phone endpoint, not an empty host directory. */
    public static List<Host> directory(String origin,String user,String password)throws Exception {
        OkHttpClient client=new OkHttpClient.Builder().followRedirects(false).followSslRedirects(false).connectTimeout(15,TimeUnit.SECONDS).readTimeout(15,TimeUnit.SECONDS).build();
        Request.Builder request=new Request.Builder().url(origin+"/_remoteconsole/devices").header("Origin",origin);
        if(!user.isEmpty()&&origin.startsWith("https://"))request.header("Authorization",Credentials.basic(user,password));
        try(Response response=client.newCall(request.build()).execute()){
            int code=response.code();
            if(code==401&&response.header("WWW-Authenticate","").toLowerCase(Locale.ROOT).contains("basic"))throw new IOException("請喺進階設定核對外網帳戶同密碼，再載入電話清單。");
            if(code==301||code==302||code==303||code==307||code==308||code==401||code==404)return null;
            if(code!=200)throw new IOException("未能取得電話清單（HTTP "+code+"）。");
            if(response.header("Content-Type","").contains("text/html"))return null;
            byte[] data=response.peekBody(128*1024+1).bytes();if(data.length>128*1024)throw new IOException("電話清單太大。");
            JSONObject object=new JSONObject(new String(data,StandardCharsets.UTF_8));
            if(!object.has("registry_version"))return null;
            JSONArray items=object.optJSONArray("devices");
            if(object.optInt("registry_version")!=1||items==null||items.length()==0||items.length()>32)throw new IOException("電話清單版本未能識別，請更新半雲電話。");
            ArrayList<Host> hosts=new ArrayList<>();HashSet<String> seen=new HashSet<>();
            for(int i=0;i<items.length();i++){
                JSONObject item=items.getJSONObject(i);String id=item.getString("id"),name=item.getString("name");Object online=item.get("online");
                if(!id.matches("[a-z][a-z0-9_-]{0,31}")||!seen.add(id)||name.isEmpty()||name.codePointCount(0,name.length())>80||!(online instanceof Boolean))throw new IOException("電話清單包含無效裝置。");
                hosts.add(new Host(id,name,(Boolean)online));
            }
            return hosts;
        }
    }
    private final HashMap<Integer,int[]> activeTouches=new HashMap<>();
    private PeerConnection peer;
    private DataChannel ordered,unordered;
    private WebSocket socket;
    private VideoTrack video;
    private AudioTrack audio;
    private boolean stopped,connected,metadata,canControl,canClipboard,muted,offerSent,negotiationStarted;
    private volatile boolean closed;
    private JSONObject device,options;
    private long lastFrames,lastBytes,lastStatsTime;
    private int width=1080,height=2400,requestedFps;
    private final Runnable deadline=()->enqueue(()->fail("連線逾時，請檢查受控電話同網絡，再撳重新連接。"));
    private final Runnable ticker=new Runnable(){public void run(){enqueue(()->{if(peer!=null&&!stopped){peer.getStats(NativeSession.this::receiveStats);main.postDelayed(this,1000);}});}};
    private void enqueue(Runnable action){if(!closed)try{queue.execute(()->{if(!stopped)try{action.run();}catch(Exception e){fail("連線未完成，請重新連接。"+safeError(e));}});}catch(RejectedExecutionException ignored){}}
    private static String safeError(Exception e){return e instanceof java.net.UnknownHostException?"（無法解析網址）":"";}
    private void ui(Runnable action){main.post(()->{if(!closed&&!cancelling)action.run();});}
    public NativeSession(Context context,String origin,String basicUser,String basicPassword,VideoSink renderer,Listener listener){
        this(context,origin,basicUser,basicPassword,null,renderer,listener);
    }
    public NativeSession(Context context,String origin,String basicUser,String basicPassword,String selectedDevice,VideoSink renderer,Listener listener){
        eglContext(context);this.origin=origin;this.renderer=renderer;this.listener=listener;this.routingDevice=selectedDevice;
        credentialStore=new ControllerCredentialStore(context.getApplicationContext());authorizationUser=basicUser;authorizationPassword=basicPassword;
        final List<Cookie> cookies=new ArrayList<>();
        CookieJar jar=new CookieJar(){
            public synchronized void saveFromResponse(HttpUrl url,List<Cookie> list){for(Cookie c:list){cookies.removeIf(old->old.name().equals(c.name())&&old.domain().equals(c.domain())&&old.path().equals(c.path()));cookies.add(c);}}
            public synchronized List<Cookie> loadForRequest(HttpUrl url){List<Cookie> result=new ArrayList<>();for(Cookie c:cookies)if(c.matches(url)&&c.expiresAt()>System.currentTimeMillis())result.add(c);return result;}
        };
        http=new OkHttpClient.Builder().cookieJar(jar).followRedirects(false).followSslRedirects(false)
            .connectTimeout(15,TimeUnit.SECONDS).readTimeout(20,TimeUnit.SECONDS)
            .addInterceptor(chain->{Request.Builder request=chain.request().newBuilder().header("Origin",origin);if(routingDevice!=null)request.header("X-RemoteConsole-Device",routingDevice);if(controllerCredential!=null)request.header("X-RemoteConsole-Controller",controllerCredential);if(!basicUser.isEmpty()&&origin.startsWith("https://"))request.header("Authorization",Credentials.basic(basicUser,basicPassword));return chain.proceed(request.build());}).build();
    }
    private JSONObject request(String path,JSONObject body)throws Exception{
        Request.Builder builder=new Request.Builder().url(origin+path);
        if(body!=null)builder.post(RequestBody.create(body.toString(),MediaType.get("application/json")));
        try(Response response=http.newCall(builder.build()).execute()){
            if(response.code()==429)throw new SessionException("配對次數過多，請按伺服器指定時間稍後再試。");
            if(response.code()==401)throw new SessionException(path.equals("/api/unlock")?"外網帳戶未通過驗證，請檢查進階登入資料。":controllerCredential!=null?"控制端授權已失效，請重新連接電話。":"配對已過期，請核對進階設定嘅舊版 PIN。");
            if(response.code()!=200)throw new SessionException("電話暫時未能連接（HTTP "+response.code()+"）。");
            if(response.body()==null)throw new IOException();
            return new JSONObject(response.body().string());
        }
    }
    private static class SessionException extends Exception{SessionException(String message){super(message);}}
    public void connect(String pin,int fps,int size,int bitrate,boolean relay,boolean mute){
        muted=mute;requestedFps=fps;
        ui(()->listener.state(pin.isEmpty()?"正在驗證控制端":"正在配對",false));
        enqueue(()->{
            try{
                if(pin.isEmpty()){if(!authorizeController())return;}
                else {JSONObject result=request("/api/unlock",new JSONObject().put("pin",pin));if(!"success".equals(result.optString("result")))throw new SessionException("配對 PIN 唔正確。餘下 "+result.optInt("leftTries")+" 次，請核對後再試。");}
                if(cancelling)return;
                JSONArray devices=request("/api/device/list",null).optJSONArray("devices");
                if(devices!=null)for(int i=0;i<devices.length();i++){JSONObject item=devices.getJSONObject(i);if("android".equals(item.optString("device_type"))){device=item;break;}}
                if(device==null)throw new SessionException("未搵到在線嘅 Android 電話。");
                String name=device.optString("name",device.optString("device_id","Android"));ui(()->listener.device(name));
                options=new JSONObject().put("video_codec","h264").put("audio_codec","opus").put("video_bit_rate",String.valueOf(bitrate)).put("max_size",String.valueOf(size)).put("max_fps",String.valueOf(fps)).put("audio","true").put("control","true").put("video_codec_options","profile=1");
                JSONObject shared=request("/api/screen/sessions",null).optJSONObject("stream_options");
                if(shared!=null&&shared.length()>0){options=shared;requestedFps=shared.optInt("max_fps",requestedFps);ui(()->listener.message("已沿用其他連線嘅共用畫質。"));}
                JSONObject config=request("/api/ice-config",null);List<PeerConnection.IceServer> servers=new ArrayList<>();
                JSONArray ice=config.getJSONArray("iceServers");
                for(int i=0;i<ice.length();i++){
                    JSONObject item=ice.getJSONObject(i);List<String> urls=new ArrayList<>();Object raw=item.get("urls");
                    if(raw instanceof JSONArray){JSONArray a=(JSONArray)raw;for(int j=0;j<a.length();j++)urls.add(a.getString(j));}else urls.add(raw.toString());
                    servers.add(PeerConnection.IceServer.builder(urls).setUsername(item.optString("username")).setPassword(item.optString("credential")).createIceServer());
                }
                PeerConnection.RTCConfiguration rtc=new PeerConnection.RTCConfiguration(servers);rtc.sdpSemantics=PeerConnection.SdpSemantics.UNIFIED_PLAN;
                if(relay||"relay".equals(config.optString("iceTransportPolicy")))rtc.iceTransportsType=PeerConnection.IceTransportsType.RELAY;
                peer=factory.createPeerConnection(rtc,observer);if(peer==null)throw new IOException("No peer");
                peer.addTransceiver(MediaStreamTrack.MediaType.MEDIA_TYPE_VIDEO,new RtpTransceiver.RtpTransceiverInit(RtpTransceiver.RtpTransceiverDirection.RECV_ONLY));
                peer.addTransceiver(MediaStreamTrack.MediaType.MEDIA_TYPE_AUDIO,new RtpTransceiver.RtpTransceiverInit(RtpTransceiver.RtpTransceiverDirection.RECV_ONLY));
                ordered=peer.createDataChannel("control-ordered",new DataChannel.Init());ordered.registerObserver(channelObserver);
                DataChannel.Init unreliable=new DataChannel.Init();unreliable.ordered=false;unordered=peer.createDataChannel("control-unordered",unreliable);unordered.registerObserver(channelObserver);
                ui(()->listener.state("正在建立聲畫連線",false));main.postDelayed(deadline,35000);
                peer.createOffer(new SDP(){@Override public void onCreateSuccess(SessionDescription offer){enqueue(()->peer.setLocalDescription(new SDP(){@Override public void onSetSuccess(){enqueue(()->{negotiationStarted=true;if(peer.iceGatheringState()==PeerConnection.IceGatheringState.COMPLETE)sendOffer();main.postDelayed(()->enqueue(NativeSession.this::sendOffer),8000);});}},offer));}},new MediaConstraints());
            }catch(SessionException e){fail(e.getMessage());}catch(Exception e){fail("連線未完成，請核對網址同網絡。"+safeError(e));}
        });
    }
    private void sendOffer(){
        if(offerSent||!negotiationStarted||peer==null||peer.getLocalDescription()==null)return;offerSent=true;
        socket=http.newWebSocket(new Request.Builder().url(origin.replaceFirst("^http","ws")+"/screen/ws").build(),new WebSocketListener(){
            @Override public void onOpen(WebSocket ws,Response response){enqueue(()->{try{JSONObject config=new JSONObject().put("device_type","android").put("device_id",device.getString("device_id")).put("device_ip",device.optString("ip")).put("device_port",device.opt("port").toString()).put("av_sync",false).put("driver_config",options).put("sdp",peer.getLocalDescription().description);ws.send(config.toString());}catch(Exception e){fail("無法建立電話連線。");}});}
            @Override public void onMessage(WebSocket ws,String text){enqueue(()->signal(text));}
            @Override public void onFailure(WebSocket ws,Throwable error,Response response){enqueue(()->fail("訊號連線中斷，請重新連接。"));}
            @Override public void onClosed(WebSocket ws,int code,String reason){enqueue(()->fail("電話已中斷連線。"));}
        });
    }
    private void signal(String text){try{
        JSONObject message=new JSONObject(text);
        if("error".equals(message.optString("status"))){fail("電話未能開始串流，請稍後重新連接。");return;}
        if("webrtc_init".equals(message.optString("stage"))){String sdp=message.getString("sdp").replace("\r\n","\n").replace('\r','\n').trim().replace("\n","\r\n")+"\r\n";peer.setRemoteDescription(new SDP(),new SessionDescription(SessionDescription.Type.ANSWER,sdp));}
        if("webrtc_metainfo".equals(message.optString("stage"))){
            JSONObject caps=message.getJSONObject("capabilities");canControl=caps.optBoolean("can_control");canClipboard=caps.optBoolean("can_clipboard");metadata=true;
            JSONObject media=message.optJSONObject("media_meta");if(media!=null){width=media.optInt("width",width);height=media.optInt("height",height);int w=width,h=height;ui(()->listener.dimensions(w,h));}
            refreshState();main.post(ticker);
        }
    }catch(Exception e){fail("電話傳回嘅連線資料無效。");}}
    private void refreshState(){boolean ready=connected&&metadata&&ordered!=null&&ordered.state()==DataChannel.State.OPEN;ui(()->listener.state(ready?"已連接 · "+(canControl?"可操作":"只限觀看"):"正在連接",ready&&canControl));if(ready){main.removeCallbacks(deadline);sendRaw(new byte[]{16});}}
    private void track(MediaStreamTrack t){
        if(t instanceof VideoTrack&&video!=t){if(video!=null)video.removeSink(renderer);video=(VideoTrack)t;video.addSink(renderer);}
        if(t instanceof AudioTrack){audio=(AudioTrack)t;audio.setEnabled(!muted);}
    }
    private final PeerConnection.Observer observer=new PeerConnection.Observer(){
        public void onSignalingChange(PeerConnection.SignalingState state){}
        public void onIceConnectionChange(PeerConnection.IceConnectionState state){enqueue(()->{if(state==PeerConnection.IceConnectionState.CONNECTED||state==PeerConnection.IceConnectionState.COMPLETED){connected=true;refreshState();}else if(state==PeerConnection.IceConnectionState.FAILED){fail("聲畫連線失敗，可以開啟中繼模式再試。");}else if(state==PeerConnection.IceConnectionState.DISCONNECTED){connected=false;releaseTouchesNow();ui(()->listener.state("網絡中斷 · 正在恢復",false));main.postDelayed(()->enqueue(()->{if(!connected)fail("網絡已中斷，請重新連接。");}),10000);}});}
        public void onIceConnectionReceivingChange(boolean receiving){}
        public void onIceGatheringChange(PeerConnection.IceGatheringState state){if(state==PeerConnection.IceGatheringState.COMPLETE)enqueue(NativeSession.this::sendOffer);}
        public void onIceCandidate(IceCandidate candidate){}
        public void onIceCandidatesRemoved(IceCandidate[] candidates){}
        public void onAddStream(MediaStream stream){enqueue(()->{for(VideoTrack t:stream.videoTracks)track(t);for(AudioTrack t:stream.audioTracks)track(t);});}
        public void onRemoveStream(MediaStream stream){}
        public void onDataChannel(DataChannel channel){}
        public void onRenegotiationNeeded(){}
        public void onAddTrack(RtpReceiver receiver,MediaStream[] streams){enqueue(()->track(receiver.track()));}
        public void onTrack(RtpTransceiver transceiver){enqueue(()->track(transceiver.getReceiver().track()));}
    };
    private final DataChannel.Observer channelObserver=new DataChannel.Observer(){
        public void onBufferedAmountChange(long amount){}
        public void onStateChange(){enqueue(NativeSession.this::refreshState);}
        public void onMessage(DataChannel.Buffer buffer){ByteBuffer data=buffer.data;byte[] bytes=new byte[data.remaining()];data.get(bytes);enqueue(()->{if(bytes.length<1)return;if(bytes[0]==0x64)ui(()->listener.message(new String(bytes,1,bytes.length-1,StandardCharsets.UTF_8)));if(bytes[0]==0x17)ui(()->listener.clipboard(new String(bytes,1,bytes.length-1,StandardCharsets.UTF_8)));});}
    };
    private class SDP implements SdpObserver{
        public void onCreateSuccess(SessionDescription sdp){} public void onSetSuccess(){}
        public void onCreateFailure(String error){enqueue(()->fail("未能建立 WebRTC 聲畫描述。"));}
        public void onSetFailure(String error){enqueue(()->fail("聲畫格式未能配對。"));}
    }
    private void receiveStats(RTCStatsReport report){enqueue(()->{
        long frames=0,bytes=0;double fps=0,rtt=0;String pairId="",localId="",remoteId="",route="P2P";Map<String,RTCStats> all=report.getStatsMap();
        for(RTCStats stat:all.values()){
            Map<String,Object> m=stat.getMembers();
            if("inbound-rtp".equals(stat.getType())&&("video".equals(m.get("kind"))||"video".equals(m.get("mediaType")))){frames=number(m.get("framesDecoded")).longValue();bytes=number(m.get("bytesReceived")).longValue();fps=number(m.get("framesPerSecond")).doubleValue();}
            if("transport".equals(stat.getType())&&m.get("selectedCandidatePairId")!=null)pairId=m.get("selectedCandidatePairId").toString();
        }
        RTCStats pair=all.get(pairId);if(pair!=null){Map<String,Object> m=pair.getMembers();rtt=number(m.get("currentRoundTripTime")).doubleValue()*1000;localId=String.valueOf(m.get("localCandidateId"));remoteId=String.valueOf(m.get("remoteCandidateId"));}
        for(String id:new String[]{localId,remoteId}){RTCStats c=all.get(id);if(c!=null&&"relay".equals(c.getMembers().get("candidateType")))route="TURN 中繼";}
        long now=System.currentTimeMillis();double elapsed=lastStatsTime==0?1:Math.max(.1,(now-lastStatsTime)/1000.0);double mbps=Math.max(0,(bytes-lastBytes)*8/elapsed/1e6);if(fps==0&&lastStatsTime!=0)fps=Math.max(0,(frames-lastFrames)/elapsed);
        lastStatsTime=now;lastFrames=frames;lastBytes=bytes;
        String label=String.format(Locale.ROOT,"%s · %.0f ms\n%.0f / %d FPS · %.1f Mbps",route,rtt,fps,requestedFps,mbps);ui(()->listener.stats(label));
        Log.i("RemoteNative","decoded="+frames+" bytes="+bytes+" control="+(ordered!=null&&ordered.state()==DataChannel.State.OPEN)+" route="+route);
    });}
    private static Number number(Object value){return value instanceof Number?(Number)value:0;}
    private boolean sendRaw(byte[] packet){return ordered!=null&&ordered.state()==DataChannel.State.OPEN&&ordered.bufferedAmount()<256000&&ordered.send(new DataChannel.Buffer(ByteBuffer.wrap(packet),true));}
    public void key(int code){enqueue(()->{if(canControl&&connected){sendRaw(ControlPacket.key(0,code));sendRaw(ControlPacket.key(1,code));}});}
    public void touch(int action,int id,int x,int y){enqueue(()->{if(!canControl||!connected)return;if(action==0)activeTouches.put(id,new int[]{x,y});else if(!activeTouches.containsKey(id))return;else activeTouches.put(id,new int[]{x,y});sendRaw(ControlPacket.touch(action,id,x,y));if(action==1)activeTouches.remove(id);});}
    private void releaseTouchesNow(){for(Map.Entry<Integer,int[]> t:activeTouches.entrySet())sendRaw(ControlPacket.touch(1,t.getKey(),t.getValue()[0],t.getValue()[1]));activeTouches.clear();}
    public void releaseTouches(){enqueue(this::releaseTouchesNow);}
    public void rotate(){enqueue(()->{if(canControl)sendRaw(new byte[]{11});});}
    public void requestFrame(){enqueue(()->sendRaw(new byte[]{16}));}
    public void paste(String text){enqueue(()->{if(!canClipboard||!canControl){ui(()->listener.message("呢部電話暫時未支援文字貼上。"));return;}try{sendRaw(ControlPacket.clipboard(text));}catch(IllegalArgumentException e){ui(()->listener.message(e.getMessage()));}});}
    public void requestClipboard(){enqueue(()->{if(canClipboard)sendRaw(new byte[]{8,0});});}
    public void mute(boolean value){enqueue(()->{muted=value;if(audio!=null)audio.setEnabled(!value);});}
    private void fail(String reason){if(stopped)return;ui(()->listener.state(reason,false));stopNow();}
    private void stopNow(){releaseTouchesNow();stopped=true;main.removeCallbacks(deadline);main.removeCallbacks(ticker);if(socket!=null){socket.close(1000,"disconnect");socket=null;}if(video!=null){video.removeSink(renderer);video=null;}if(peer!=null)peer.close();if(ordered!=null){ordered.unregisterObserver();ordered.dispose();ordered=null;}if(unordered!=null){unordered.unregisterObserver();unordered.dispose();unordered=null;}if(peer!=null){peer.dispose();peer=null;}http.dispatcher().cancelAll();http.connectionPool().evictAll();}
    public void close(){if(closed||cancelling)return;cancelling=true;cancelApproval();http.dispatcher().cancelAll();try{queue.execute(()->{if(!stopped)stopNow();closed=true;queue.shutdown();});}catch(RejectedExecutionException ignored){}}

    private static final class AuthResponse {final int status;final JSONObject body;AuthResponse(int status,JSONObject body){this.status=status;this.body=body;}}
    private AuthResponse authorization(String path,JSONObject body)throws Exception {
        Request.Builder builder=new Request.Builder().url(origin+"/_remoteconsole/trust/"+path);
        if(body!=null)builder.post(RequestBody.create(body.toString(),MediaType.get("application/json")));
        try(Response response=http.newCall(builder.build()).execute()){
            if(response.code()==401&&response.header("WWW-Authenticate","").toLowerCase(Locale.ROOT).contains("basic"))throw new SessionException("請核對進階設定嘅外網帳戶同密碼。");
            byte[] bytes=response.peekBody(131073).bytes();if(bytes.length>131072)throw new IOException();
            return new AuthResponse(response.code(),new JSONObject(new String(bytes,StandardCharsets.UTF_8)));
        }
    }
    private boolean authorizeController()throws Exception {
        AuthResponse status=authorization("status",null);
        if(status.status!=200||!status.body.optBoolean("supported"))throw new SessionException("呢個入口未支援控制端授權，請更新外網主機；舊版區域網配對可喺進階設定填寫 PIN。");
        String stored=credentialStore.load(origin);
        if(stored!=null){AuthResponse check=authorization("session",new JSONObject().put("credential",stored));if(check.status==200&&check.body.optBoolean("authenticated")){controllerCredential=stored;return true;}if(check.status!=401)throw new SessionException("暫時未能驗證控制端，請稍後再連接。");credentialStore.remove(origin);}
        String name=android.os.Build.MANUFACTURER+" "+android.os.Build.MODEL;
        AuthResponse created=authorization("requests",new JSONObject().put("name",name).put("platform","android").put("kind","native"));
        if(created.status!=201)throw new SessionException("暫時未能提出授權要求，請稍後再試。");
        approvalRequest=created.body.getString("request_id");approvalSecret=created.body.getString("request_secret");String code=created.body.getString("approval_code");
        ui(()->{listener.state("等候受控電話批准",false);listener.approval(name,code);});
        long until=android.os.SystemClock.elapsedRealtime()+300000;int interval=Math.min(10,Math.max(2,created.body.optInt("interval",2)));
        while(!cancelling&&!closed&&android.os.SystemClock.elapsedRealtime()<until){
            Thread.sleep(interval*1000L);if(cancelling||closed)return false;
            AuthResponse poll=authorization("poll",new JSONObject().put("request_id",approvalRequest).put("request_secret",approvalSecret));
            if(poll.status==200&&"approved".equals(poll.body.optString("state"))){String credential=poll.body.getString("credential");credentialStore.save(origin,credential);controllerCredential=credential;approvalRequest=null;approvalSecret=null;ui(()->listener.state("已信任 · 正在連接電話",false));return true;}
            if(poll.status==202||poll.status==429){interval=Math.min(10,Math.max(2,poll.body.optInt("interval",2)));continue;}
            approvalRequest=null;approvalSecret=null;throw new SessionException(poll.status==403?"受控電話已拒絕授權。":"授權要求已過期，請重新連接。");
        }
        cancelApproval();if(cancelling||closed)return false;throw new SessionException("授權要求已過期，請重新連接。");
    }
    private void cancelApproval(){
        String id=approvalRequest,secret=approvalSecret;approvalRequest=null;approvalSecret=null;if(id==null||secret==null)return;
        try{
            Request.Builder request=new Request.Builder().url(origin+"/_remoteconsole/trust/cancel").header("Origin",origin).post(RequestBody.create(new JSONObject().put("request_id",id).put("request_secret",secret).toString(),MediaType.get("application/json")));
            if(!authorizationUser.isEmpty()&&origin.startsWith("https://"))request.header("Authorization",Credentials.basic(authorizationUser,authorizationPassword));
            OkHttpClient cleanup=new OkHttpClient.Builder().connectTimeout(4,TimeUnit.SECONDS).readTimeout(4,TimeUnit.SECONDS).build();
            cleanup.newCall(request.build()).enqueue(new Callback(){public void onFailure(Call call,IOException error){}public void onResponse(Call call,Response response){response.close();cleanup.dispatcher().executorService().shutdown();cleanup.connectionPool().evictAll();}});
        }catch(Exception ignored){}
    }
}
