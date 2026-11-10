package com.sing.halfcloud.controller;

import android.app.*;
import android.os.Bundle;
import android.content.*;
import android.content.res.ColorStateList;
import android.graphics.*;
import android.graphics.drawable.ColorDrawable;
import android.net.Uri;
import android.view.*;
import android.view.inputmethod.InputMethodManager;
import android.widget.*;
import org.json.*;
import org.webrtc.*;
import java.util.*;

/** Custom native UI; the media receiver and input protocol live in NativeSession. */
public final class ControllerActivity extends Activity {
    private ConsoleStyle ui;
    private LinearLayout root,nav,header;
    private FrameLayout stage;
    private TextView title,status,stats,approvalCode,approvalName;
    private LinearLayout approvalPanel;
    private String legacyPin="";
    private ImageButton audioButton;
    private EditText address;
    private TextView hostChoice;
    private String selectedDeviceID,selectedDeviceName,selectionOrigin="";
    private boolean loadingHosts,directoryKnown;
    private SurfaceViewRenderer renderer;
    private View sheetScrim;
    private final HashSet<Dialog> openSheets=new HashSet<>();
    private boolean switchingSheet;
    private NativeSession session;
    private NativeTools nativeTools;
    private boolean pickingDocument,adaptive=true,autoReconnect=true;
    private String relayRegion="auto";
    private String sessionPin="",basicUser="",basicPassword="",currentUrl="",deviceName="Android";
    private int epoch,videoW=1080,videoH=2400,fps=60,maxSize=1920,bitrate=8000000;
    private boolean ready,muted=true,relay,fullscreen;
    private final ArrayList<String> saved=new ArrayList<>();
    private final HashMap<Integer,Integer> pointers=new HashMap<>();
    private int dp(float value){return ui.dp(value);}
    @Override public void onCreate(Bundle state){super.onCreate(state);ui=new ConsoleStyle(this);getWindow().setStatusBarColor(ConsoleStyle.BG);getWindow().setNavigationBarColor(ConsoleStyle.BG);getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        try{JSONArray list=new JSONArray(getPreferences(0).getString("devices","[]"));for(int i=0;i<list.length();i++)saved.add(list.getString(i));}catch(Exception ignored){}
        fps=getPreferences(0).getInt("fps",60);maxSize=getPreferences(0).getInt("size",1920);bitrate=getPreferences(0).getInt("bitrate",8000000);relay=getPreferences(0).getBoolean("relay",false);adaptive=getPreferences(0).getBoolean("adaptive",true);autoReconnect=getPreferences(0).getBoolean("reconnect",true);relayRegion=getPreferences(0).getString("relayRegion","auto");showDevices();}
    private void persist(){getPreferences(0).edit().putString("devices",new JSONArray(saved).toString()).putInt("fps",fps).putInt("size",maxSize).putInt("bitrate",bitrate).putBoolean("relay",relay).putBoolean("adaptive",adaptive).putBoolean("reconnect",autoReconnect).putString("relayRegion",relayRegion).apply();}
    private void closeSession(){if(nativeTools!=null){nativeTools.close();nativeTools=null;}epoch++;ready=false;pointers.clear();if(session!=null){session.close();session=null;}if(renderer!=null){renderer.release();renderer=null;}getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);}
    private void showDevices(){closeSession();sessionPin="";fullscreen=false;getWindow().getDecorView().setSystemUiVisibility(0);
        ScrollView scroll=new ScrollView(this);scroll.setFillViewport(true);scroll.setVerticalScrollBarEnabled(false);scroll.setBackground(ui.backdrop());root=ui.column();root.setFocusableInTouchMode(true);root.setDescendantFocusability(android.view.ViewGroup.FOCUS_BEFORE_DESCENDANTS);root.requestFocus();root.setPadding(dp(22),dp(15),dp(22),dp(24));scroll.addView(root);setContentView(scroll);
        LinearLayout brand=ui.row();brand.addView(ui.brandMark(44));LinearLayout identity=ui.column();identity.setPadding(dp(11),0,0,0);identity.addView(ui.text("半雲電話",17,ConsoleStyle.INK,true));ui.gap(identity,4);identity.addView(ui.text("控制端",10,ConsoleStyle.MUTED,false));brand.addView(identity,new LinearLayout.LayoutParams(0,-2,1));TextView version=ui.text("v"+BuildConfig.VERSION_NAME,10,ConsoleStyle.MINT,false);version.setTypeface(Typeface.MONOSPACE);version.setPadding(dp(11),dp(8),dp(11),dp(8));version.setBackground(ui.fill(0x149ae8ce,24));brand.addView(version);root.addView(brand);ui.gap(root,28);
        LinearLayout hero=ui.row();LinearLayout heroCopy=ui.column();heroCopy.addView(ui.text("裝置空間",30,ConsoleStyle.INK,true));ui.gap(heroCopy,8);heroCopy.addView(ui.text("你的 Android 電話，隨時連接。",13,ConsoleStyle.MUTED,false));hero.addView(heroCopy,new LinearLayout.LayoutParams(0,-2,1));hero.addView(ui.icon("devices",36,0x999ae8ce));root.addView(hero);ui.gap(root,24);
        LinearLayout card=ui.column();card.setPadding(dp(20),dp(20),dp(20),dp(16));card.setBackground(ui.panel(26));LinearLayout cardTitle=ui.row();FrameLayout phoneMark=new FrameLayout(this);phoneMark.setBackground(ui.fill(0x1289e1c5,12));phoneMark.addView(ui.icon("phone",24,ConsoleStyle.MINT),new FrameLayout.LayoutParams(dp(24),dp(24),Gravity.CENTER));cardTitle.addView(phoneMark,new LinearLayout.LayoutParams(dp(40),dp(44)));LinearLayout cardCopy=ui.column();cardCopy.setPadding(dp(12),0,0,0);cardCopy.addView(ui.text("連線入口",18,ConsoleStyle.INK,true));ui.gap(cardCopy,5);cardCopy.addView(ui.text("一個網址，連接所有受控電話",11,ConsoleStyle.MUTED,false));cardTitle.addView(cardCopy,new LinearLayout.LayoutParams(0,-2,1));card.addView(cardTitle);ui.gap(card,20);
        address=ui.input("https://你的電話網址",false);address.setContentDescription("電話網址");address.setTextSize(12);address.setTypeface(Typeface.MONOSPACE);address.setText(saved.isEmpty()?currentUrl:saved.get(0));ui.field(card,"電話網址","link",address);ui.gap(card,17);
        LinearLayout hostRow=ui.row();hostRow.setPadding(dp(14),dp(12),dp(14),dp(12));hostRow.setBackground(ui.ripple(ui.inset()));hostRow.addView(ui.icon("phone",18,ConsoleStyle.MINT));hostChoice=ui.text(selectedDeviceName==null?"揀受控電話":selectedDeviceName,13,ConsoleStyle.INK,true);hostChoice.setPadding(dp(10),0,0,0);hostRow.addView(hostChoice,new LinearLayout.LayoutParams(0,-2,1));hostRow.addView(ui.icon("devices",16,ConsoleStyle.MUTED));hostRow.setContentDescription("選擇受控電話");hostRow.setOnClickListener(v->lookupHosts(address.getText().toString(),null));card.addView(hostRow);ui.gap(card,17);
        card.addView(ui.text("首次批准控制端一次，之後自動登入。",12,ConsoleStyle.MUTED,false));ui.gap(card,20);
        LinearLayout connect=ui.primary("連接電話","arrow");card.addView(connect);connect.setOnClickListener(v->connect(address.getText().toString(),legacyPin));address.setOnEditorActionListener((v,action,event)->{if(action==android.view.inputmethod.EditorInfo.IME_ACTION_GO){connect.performClick();return true;}return false;});ui.gap(card,8);
        LinearLayout quick=ui.row();LinearLayout quality=ui.row();quality.addView(ui.icon("settings",15,ConsoleStyle.MUTED));TextView q=ui.text("畫質與連線",11,ConsoleStyle.MUTED,false);q.setPadding(dp(6),0,0,0);quality.addView(q);quality.setContentDescription("畫質與連線");quality.setBackground(ui.ripple(ui.fill(Color.TRANSPARENT,12)));quality.setOnClickListener(v->settings());quick.addView(quality,new LinearLayout.LayoutParams(0,dp(40),1));TextView advanced=ui.text("進階  ›",11,ConsoleStyle.MUTED,false);advanced.setGravity(Gravity.RIGHT|Gravity.CENTER_VERTICAL);advanced.setContentDescription("進階登入");advanced.setOnClickListener(v->basicAuth());quick.addView(advanced,new LinearLayout.LayoutParams(dp(72),dp(40)));card.addView(quick);root.addView(card);ui.gap(root,25);
        LinearLayout savedTitle=ui.row();savedTitle.addView(ui.text("常用入口",14,ConsoleStyle.INK,true),new LinearLayout.LayoutParams(0,-2,1));savedTitle.addView(ui.text(saved.isEmpty()?"尚未儲存":saved.size()+" 個",10,ConsoleStyle.MUTED,false));root.addView(savedTitle);ui.gap(root,12);
        if(saved.isEmpty()){LinearLayout empty=ui.row();empty.setPadding(dp(15),dp(17),dp(15),dp(17));empty.setBackground(ui.inset());empty.addView(ui.icon("plus",23,ConsoleStyle.MUTED));LinearLayout copy=ui.column();copy.setPadding(dp(13),0,0,0);copy.addView(ui.text("新增第一個入口",12,ConsoleStyle.INK,false));ui.gap(copy,5);copy.addView(ui.text("先輸入網址，再選擇受控電話",10,ConsoleStyle.MUTED,false));empty.addView(copy,new LinearLayout.LayoutParams(0,-2,1));root.addView(empty);}
        for(String url:new ArrayList<>(saved)){LinearLayout item=ui.row();item.setPadding(dp(16),dp(16),dp(16),dp(16));item.setBackground(ui.ripple(ui.panel(20)));item.addView(ui.icon("phone",26,ConsoleStyle.MINT));LinearLayout copy=ui.column();copy.setPadding(dp(12),0,0,0);copy.addView(ui.text("遙控入口",13,ConsoleStyle.INK,true));ui.gap(copy,5);TextView host=ui.text(Uri.parse(url).getHost(),10,ConsoleStyle.MUTED,false);host.setSingleLine(true);host.setEllipsize(android.text.TextUtils.TruncateAt.MIDDLE);copy.addView(host);item.addView(copy,new LinearLayout.LayoutParams(0,-2,1));item.addView(ui.icon("arrow",16,ConsoleStyle.MUTED));root.addView(item,new LinearLayout.LayoutParams(-1,-2));ui.gap(root,8);item.setContentDescription("已儲存電話 "+Uri.parse(url).getHost());item.setOnClickListener(v->{address.setText(url);root.requestFocus();((InputMethodManager)getSystemService(INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(address.getWindowToken(),0);});item.setOnLongClickListener(v->{new AlertDialog.Builder(this).setMessage("移除呢部已儲存電話？").setNegativeButton("取消",null).setPositiveButton("移除",(d,w)->{saved.remove(url);persist();showDevices();}).show();return true;});}
        ui.gap(root,24);LinearLayout benefits=ui.row();String[] icons={"touch","wave","devices"},labels={"觸控遙控","聲音同步","多裝置"};for(int i=0;i<3;i++){LinearLayout benefit=ui.column();benefit.setGravity(Gravity.CENTER);benefit.addView(ui.icon(icons[i],18,0xb389e1c5));ui.gap(benefit,9);benefit.addView(ui.text(labels[i],10,ConsoleStyle.MUTED,false));benefits.addView(benefit,new LinearLayout.LayoutParams(0,-2,1));}root.addView(benefits);ui.gap(root,24);TextView footer=ui.text("信任一次 · 之後免 PIN · 隨時撤銷",10,0xff6c8791,false);footer.setGravity(Gravity.CENTER);root.addView(footer);
    }
    private String validate(String raw)throws Exception{String input=raw.trim();if(!input.contains("://")){Uri u=Uri.parse("http://"+input);input=(EndpointPolicy.isPrivateHost(u.getHost())?"http://":"https://")+input;}Uri u=Uri.parse(input);String scheme=u.getScheme();if(!("http".equals(scheme)||"https".equals(scheme))||u.getHost()==null||u.getUserInfo()!=null||u.getQuery()!=null||u.getFragment()!=null||!(u.getPath()==null||u.getPath().isEmpty()||u.getPath().equals("/")||u.getPath().equals("/control")))throw new Exception("請輸入有效電話網址。");if("http".equals(scheme)&&!EndpointPolicy.isPrivateHost(u.getHost()))throw new Exception("外網網址需要 HTTPS。");return u.buildUpon().path("").build().toString();}
    private void connect(String raw,String code){if(!code.isEmpty()&&!code.matches("[0-9]{6}")){toast("舊版 PIN 需要 6 位數字。");return;}lookupHosts(raw,code);}
    private void lookupHosts(String raw,String code){
        if(loadingHosts)return;
        final String origin;try{origin=validate(raw);}catch(Exception error){toast(error.getMessage());return;}
        loadingHosts=true;if(hostChoice!=null)hostChoice.setText("載入電話清單…");
        ((InputMethodManager)getSystemService(INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(getWindow().getDecorView().getWindowToken(),0);
        final int generation=epoch;final String user=basicUser,password=basicPassword;
        new Thread(()->{try{
            List<NativeSession.Host> hosts=NativeSession.directory(origin,user,password);
            runOnUiThread(()->{loadingHosts=false;if(isFinishing()||isDestroyed()||generation!=epoch)return;
                if(hosts==null&&directoryKnown&&selectionOrigin.equals(origin)){if(hostChoice!=null)hostChoice.setText(selectedDeviceName==null?"揀受控電話":selectedDeviceName);toast("入口暫時未能提供電話清單，請重新整理後再試。");return;}
                if(!selectionOrigin.equals(origin)){selectedDeviceID=null;selectedDeviceName=null;directoryKnown=false;}selectionOrigin=origin;
                if(hosts==null){selectedDeviceID=null;selectedDeviceName=null;if(hostChoice!=null)hostChoice.setText("單一電話 · 直接配對");if(code!=null)connectChosen(origin,code);else toast("呢個入口直接連接一部電話。");return;}
                directoryKnown=true;NativeSession.Host selected=null;
                for(NativeSession.Host host:hosts)if(host.id.equals(selectedDeviceID))selected=host;
                if(hosts.size()==1)selected=hosts.get(0);
                if(hostChoice!=null)hostChoice.setText(selected==null?"揀受控電話":selected.name);
                if(code!=null&&selected!=null&&selected.online){selectedDeviceID=selected.id;selectedDeviceName=selected.name;connectChosen(origin,code);}else hostPicker(hosts,origin,code);
            });
        }catch(Exception error){runOnUiThread(()->{loadingHosts=false;if(isFinishing()||isDestroyed()||generation!=epoch)return;if(hostChoice!=null)hostChoice.setText(selectedDeviceName==null?"揀受控電話":selectedDeviceName);toast(error.getMessage()==null?"未能取得電話清單。":error.getMessage());});}},"remote-host-directory").start();
    }
    private void hostPicker(List<NativeSession.Host> hosts,String origin,String code){
        LinearLayout content=ui.column();Dialog[] dialog=new Dialog[1];
        for(NativeSession.Host host:hosts){LinearLayout row=ui.row();row.setPadding(dp(18),dp(20),dp(18),dp(20));row.setBackground(ui.ripple(ui.inset()));row.addView(ui.icon("phone",25,host.online?ConsoleStyle.MINT:ConsoleStyle.MUTED));LinearLayout copy=ui.column();copy.setPadding(dp(14),0,0,0);copy.addView(ui.text(host.name,16,ConsoleStyle.INK,true));ui.gap(copy,7);copy.addView(ui.text(host.online?"已連線 · 撳入去操作":"離線 · 等候電話連線",11,host.online?ConsoleStyle.MINT:ConsoleStyle.MUTED,false));row.addView(copy,new LinearLayout.LayoutParams(0,-2,1));row.addView(ui.icon("arrow",17,ConsoleStyle.MINT));row.setEnabled(host.online);row.setAlpha(host.online?1:.55f);row.setContentDescription("受控電話 "+host.name);row.setOnClickListener(v->afterSheetDismiss(dialog[0],()->{selectedDeviceID=host.id;selectedDeviceName=host.name;selectionOrigin=origin;if(hostChoice!=null)hostChoice.setText(host.name);if(code!=null)connectChosen(origin,code);}));content.addView(row);ui.gap(content,12);}
        content.addView(ui.text("已信任控制端可喺同一入口切換電話，連線互相分開。",11,ConsoleStyle.MUTED,false));dialog[0]=sheet("揀受控電話","同一個網址，揀你想連接嘅電話",content);dialog[0].show();
    }
    private void connectChosen(String raw,String code){try{String origin=validate(raw);if(!code.isEmpty()&&!code.matches("[0-9]{6}"))throw new Exception("舊版 PIN 需要 6 位數字。");((InputMethodManager)getSystemService(INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(getWindow().getDecorView().getWindowToken(),0);sessionPin=code;currentUrl=origin+"/control";saved.remove(currentUrl);saved.add(0,currentUrl);while(saved.size()>12)saved.remove(saved.size()-1);persist();deviceName=selectedDeviceName==null?"Android":selectedDeviceName;showRemote();int generation=epoch;SurfaceViewRenderer sink=renderer;
        session=new NativeSession(this,origin,basicUser,basicPassword,selectedDeviceID,frame->{if(generation!=epoch)return;int w=frame.getRotatedWidth(),h=frame.getRotatedHeight();sink.onFrame(frame);if(w!=videoW||h!=videoH)runOnUiThread(()->{if(generation==epoch){videoW=w;videoH=h;fitVideo();}});},new NativeSession.Listener(){
            public void state(String value,boolean enabled){if(generation!=epoch)return;ready=enabled;if(approvalPanel!=null&&!value.contains("批准"))approvalPanel.setVisibility(View.GONE);status.setText(value);status.setTextColor(enabled?ConsoleStyle.MINT:ConsoleStyle.MUTED);nav.setAlpha(enabled?1:.55f);}
            public void device(String name){if(generation==epoch){deviceName=selectedDeviceName==null?name:selectedDeviceName;title.setText(deviceName);}}
            public void stats(String value){if(generation==epoch)stats.setText(value);}
            public void dimensions(int w,int h){if(generation==epoch){videoW=w;videoH=h;fitVideo();}}
            public void message(String value){if(generation==epoch)toast(value);}
            public void clipboard(String value){if(generation==epoch)clipboardSheet(value);}
            public void approval(String name,String code){if(generation==epoch&&approvalPanel!=null){approvalCode.setText(code);approvalName.setText(name);approvalPanel.setVisibility(View.VISIBLE);}}
        });session.policy(relayRegion,adaptive,autoReconnect);nativeTools=new NativeTools(this,ui,session);session.connect(code,fps,maxSize,bitrate,relay,muted);
    }catch(Exception e){toast(e.getMessage());}}
    private void showRemote(){closeSession();root=ui.column();root.setPadding(dp(12),dp(8),dp(12),dp(8));root.setBackground(ui.backdrop());setContentView(root);getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        header=ui.row();header.setPadding(dp(6),dp(5),dp(6),dp(5));header.setBackground(ui.panel(23));ImageButton back=ui.iconButton("back","電話清單");header.addView(back,new LinearLayout.LayoutParams(dp(36),dp(42)));back.setOnClickListener(v->showDevices());LinearLayout copy=ui.column();copy.setPadding(dp(6),0,dp(6),0);title=ui.text(deviceName,14,ConsoleStyle.INK,true);title.setSingleLine(true);title.setEllipsize(android.text.TextUtils.TruncateAt.END);status=ui.text("正在連接",9,ConsoleStyle.MUTED,false);status.setMaxLines(2);copy.addView(title);ui.gap(copy,4);copy.addView(status);header.addView(copy,new LinearLayout.LayoutParams(0,-2,1));audioButton=ui.iconButton(muted?"mute":"sound",muted?"開聲":"靜音");header.addView(audioButton,new LinearLayout.LayoutParams(dp(36),dp(42)));audioButton.setOnClickListener(v->{muted=!muted;if(session!=null)session.mute(muted);audioButton.setImageDrawable(new ConsoleStyle.Glyph(muted?"mute":"sound",ConsoleStyle.INK));audioButton.setContentDescription(muted?"開聲":"靜音");});ImageButton full=ui.iconButton("fullscreen","全螢幕");header.addView(full,new LinearLayout.LayoutParams(dp(36),dp(42)));full.setOnClickListener(v->toggleFullscreen());root.addView(header);ui.gap(root,10);
        stage=new FrameLayout(this);stage.setBackground(ui.fill(Color.BLACK,22));stage.setClipToOutline(true);root.addView(stage,new LinearLayout.LayoutParams(-1,0,1));renderer=new SurfaceViewRenderer(this);renderer.init(NativeSession.eglContext(this),null);renderer.setScalingType(RendererCommon.ScalingType.SCALE_ASPECT_FIT);renderer.setEnableHardwareScaler(true);stage.addView(renderer,new FrameLayout.LayoutParams(-1,-1,Gravity.CENTER));stage.addOnLayoutChangeListener((v,l,t,r,b,ol,ot,or,ob)->fitVideo());View touch=new View(this);touch.setContentDescription("遙控電話畫面");stage.addView(touch,new FrameLayout.LayoutParams(-1,-1));touch.setOnTouchListener(this::touch);
        approvalPanel=ui.column();approvalPanel.setPadding(dp(24),dp(24),dp(24),dp(24));approvalPanel.setBackground(ui.panel(24));approvalPanel.addView(ui.text("等候受控電話批准",18,ConsoleStyle.INK,true));ui.gap(approvalPanel,14);approvalName=ui.text("",12,ConsoleStyle.MUTED,false);approvalPanel.addView(approvalName);approvalCode=ui.text("",28,ConsoleStyle.MINT,true);approvalCode.setTypeface(Typeface.MONOSPACE);approvalCode.setGravity(Gravity.CENTER);approvalPanel.addView(approvalCode);ui.gap(approvalPanel,16);approvalPanel.addView(ui.text("喺受控電話開「半雲電話 → 控制端授權」，核對代碼同控制端名稱後批准。之後可自動登入所有電話。",12,ConsoleStyle.MUTED,false));FrameLayout.LayoutParams ap=new FrameLayout.LayoutParams(-1,-2,Gravity.CENTER);ap.setMargins(dp(18),0,dp(18),0);stage.addView(approvalPanel,ap);approvalPanel.setVisibility(View.GONE);
        stats=ui.text("等待電話畫面",10,0xffbbd1d9,false);stats.setTypeface(Typeface.MONOSPACE);stats.setPadding(dp(10),dp(4),dp(10),dp(4));stats.setTextColor(ConsoleStyle.MUTED);stats.setSingleLine(true);stats.setEllipsize(android.text.TextUtils.TruncateAt.END);FrameLayout.LayoutParams sp=new FrameLayout.LayoutParams(-2,-2,Gravity.BOTTOM|Gravity.LEFT);sp.setMargins(dp(10),0,dp(10),dp(10));root.addView(stats,new LinearLayout.LayoutParams(-1,-2));ui.gap(root,10);
        nav=ui.row();nav.setPadding(dp(6),dp(6),dp(6),dp(6));nav.setBackground(ui.panel(25));String[] labels={"返回","主畫面","最近","文字","更多"},icons={"back","home","recent","keyboard","more"};int[] keys={4,3,187};for(int i=0;i<labels.length;i++){int index=i;LinearLayout action=ui.action(labels[i],icons[i],i==4);nav.addView(action,new LinearLayout.LayoutParams(0,dp(52),1));action.setOnClickListener(v->{if(index<3){if(session!=null&&ready)session.key(keys[index]);}else if(index==3)keyboard();else more();});}root.addView(nav);nav.setAlpha(.55f);
    }
    private void toggleFullscreen(){fullscreen=!fullscreen;header.setVisibility(fullscreen?View.GONE:View.VISIBLE);getWindow().getDecorView().setSystemUiVisibility(fullscreen?5894:0);}
    private void fitVideo(){if(renderer==null||stage==null||stage.getWidth()==0||stage.getHeight()==0||videoW<=0||videoH<=0)return;double scale=Math.min((double)stage.getWidth()/videoW,(double)stage.getHeight()/videoH);int w=(int)Math.round(videoW*scale),h=(int)Math.round(videoH*scale);android.view.ViewGroup.LayoutParams old=renderer.getLayoutParams();if(old.width!=w||old.height!=h)renderer.setLayoutParams(new FrameLayout.LayoutParams(w,h,Gravity.CENTER));}
    private boolean touch(View view,MotionEvent event){if(!ready||session==null)return true;int action=event.getActionMasked(),index=event.getActionIndex();if(action==MotionEvent.ACTION_CANCEL){session.releaseTouches();pointers.clear();return true;}if(action==MotionEvent.ACTION_DOWN||action==MotionEvent.ACTION_POINTER_DOWN){if(pointers.size()>=10)return true;int[] p=ControlPacket.position(event.getX(index),event.getY(index),view.getWidth(),view.getHeight(),videoW,videoH,false);if(p==null)return true;int id=0;while(pointers.containsValue(id))id++;pointers.put(event.getPointerId(index),id);session.touch(0,id,p[0],p[1]);}else if(action==MotionEvent.ACTION_MOVE){for(int i=0;i<event.getPointerCount();i++){Integer id=pointers.get(event.getPointerId(i));if(id==null)continue;int[] p=ControlPacket.position(event.getX(i),event.getY(i),view.getWidth(),view.getHeight(),videoW,videoH,true);if(p!=null)session.touch(2,id,p[0],p[1]);}}else if(action==MotionEvent.ACTION_UP||action==MotionEvent.ACTION_POINTER_UP){Integer id=pointers.remove(event.getPointerId(index));if(id!=null){int[] p=ControlPacket.position(event.getX(index),event.getY(index),view.getWidth(),view.getHeight(),videoW,videoH,true);if(p!=null)session.touch(1,id,p[0],p[1]);}}return true;}
    private Dialog sheet(String title,String subtitle,LinearLayout content){
        if(session!=null)session.releaseTouches();
        Dialog dialog=new Dialog(this);
        LinearLayout wrap=ui.column();wrap.setPadding(dp(12),dp(8),dp(12),dp(18));
        LinearLayout card=ui.column();card.setPadding(dp(22),dp(23),dp(22),dp(22));card.setBackground(ui.sheetSurface());
        LinearLayout heading=ui.row(),copy=ui.column();
        copy.addView(ui.text(title,24,ConsoleStyle.INK,true));ui.gap(copy,8);
        copy.addView(ui.text(subtitle,11,ConsoleStyle.MUTED,false));heading.addView(copy,new LinearLayout.LayoutParams(0,-2,1));
        ImageButton close=ui.iconButton("close","關閉");close.setBackground(ui.ripple(ui.fill(0x0effffff,20)));
        heading.addView(close,new LinearLayout.LayoutParams(dp(38),dp(38)));close.setOnClickListener(v->dialog.dismiss());
        card.addView(heading);ui.gap(card,25);card.addView(content);wrap.addView(card);
        // Bound the first measurement. Resizing an already-animating window causes a jump.
        final int maxHeight=Math.max(dp(240),getResources().getDisplayMetrics().heightPixels-dp(100));
        ScrollView scroll=new ScrollView(this){
            @Override protected void onMeasure(int widthSpec,int heightSpec){
                int limit=View.MeasureSpec.getMode(heightSpec)==View.MeasureSpec.UNSPECIFIED?maxHeight:Math.min(maxHeight,View.MeasureSpec.getSize(heightSpec));
                super.onMeasure(widthSpec,View.MeasureSpec.makeMeasureSpec(limit,View.MeasureSpec.AT_MOST));
            }
        };
        scroll.setFillViewport(false);scroll.setVerticalScrollBarEnabled(false);scroll.addView(wrap);dialog.setContentView(scroll);
        Window window=dialog.getWindow();window.setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
        WindowManager.LayoutParams attrs=window.getAttributes();
        attrs.width=WindowManager.LayoutParams.MATCH_PARENT;attrs.height=WindowManager.LayoutParams.WRAP_CONTENT;
        attrs.gravity=Gravity.BOTTOM;attrs.dimAmount=0;attrs.windowAnimations=R.style.ConsoleSheetMotion;
        window.setAttributes(attrs);window.clearFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND);
        window.setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        dialog.setOnShowListener(ignored->{openSheets.add(dialog);showSheetScrim();});
        dialog.setOnDismissListener(ignored->{openSheets.remove(dialog);if(openSheets.isEmpty()&&!switchingSheet)hideSheetScrim();});
        return dialog;
    }
    private void showSheetScrim(){
        if(sheetScrim==null||sheetScrim.getParent()==null){
            FrameLayout parent=findViewById(android.R.id.content);
            sheetScrim=new View(this);sheetScrim.setBackgroundColor(Color.BLACK);sheetScrim.setAlpha(0);
            sheetScrim.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
            parent.addView(sheetScrim,new FrameLayout.LayoutParams(-1,-1));
        }
        sheetScrim.animate().cancel();sheetScrim.animate().alpha(.65f).setDuration(160).start();
    }
    private void hideSheetScrim(){
        View scrim=sheetScrim;if(scrim==null)return;
        scrim.animate().cancel();scrim.animate().alpha(0).setDuration(160).withEndAction(()->{
            if(scrim==sheetScrim&&openSheets.isEmpty()&&!switchingSheet){
                if(scrim.getParent() instanceof ViewGroup)((ViewGroup)scrim.getParent()).removeView(scrim);
                sheetScrim=null;
            }
        }).start();
    }
    private void afterSheetDismiss(Dialog dialog,Runnable action){
        Window window=dialog.getWindow();View decor=window.getDecorView();
        if(!dialog.isShowing()||Boolean.TRUE.equals(decor.getTag()))return;
        decor.setTag(Boolean.TRUE);switchingSheet=true;
        // A single activity scrim persists across both windows; the platform's
        // per-window dim surfaces otherwise fade out/in between tool panels.
        decor.animate().translationY(decor.getHeight()).setDuration(160)
            .setInterpolator(android.view.animation.AnimationUtils.loadInterpolator(this,android.R.interpolator.fast_out_linear_in))
            .withEndAction(()->{
                window.setWindowAnimations(0);
                try{if(!isFinishing()&&!isDestroyed())action.run();}finally{
                    dialog.dismiss();switchingSheet=false;if(openSheets.isEmpty())hideSheetScrim();
                }
            }).start();
    }
    private void sectionLabel(LinearLayout parent,String label,String hint){LinearLayout line=ui.row();line.addView(ui.text(label,13,ConsoleStyle.INK,true),new LinearLayout.LayoutParams(0,-2,1));line.addView(ui.text(hint,9,ConsoleStyle.MUTED,false));parent.addView(line);ui.gap(parent,12);}
    private void settings(){LinearLayout form=ui.column();sectionLabel(form,"影片流暢度","依裝置及網絡而定");ConsoleStyle.ChoiceGroup f=ui.choices(new int[]{30,60,120},new String[]{"30 FPS","60 FPS","120 FPS"},fps);form.addView(f);ui.gap(form,24);sectionLabel(form,"畫面大小","最長邊解像度");ConsoleStyle.ChoiceGroup size=ui.choices(new int[]{960,1280,1920,2560},new String[]{"960","1280","1920","2560"},maxSize);form.addView(size);ui.gap(form,24);sectionLabel(form,"位元率","更清晰會使用更多數據");ConsoleStyle.ChoiceGroup rate=ui.choices(new int[]{2000000,4000000,8000000,16000000},new String[]{"2 M","4 M","8 M","16 M"},bitrate);form.addView(rate);ui.gap(form,26);
        LinearLayout network=ui.row();network.addView(ui.icon("network",21,ConsoleStyle.MINT));LinearLayout copy=ui.column();copy.setPadding(dp(12),0,dp(8),0);copy.addView(ui.text("TURN 中繼模式",13,ConsoleStyle.INK,true));ui.gap(copy,5);copy.addView(ui.text("直連不穩定時可以開啟",10,ConsoleStyle.MUTED,false));network.addView(copy,new LinearLayout.LayoutParams(0,-2,1));Switch force=new Switch(this);force.setContentDescription("TURN 中繼模式");force.setChecked(relay);force.setThumbTintList(ColorStateList.valueOf(ConsoleStyle.MINT));force.setTrackTintList(new ColorStateList(new int[][]{new int[]{android.R.attr.state_checked},new int[]{}},new int[]{0x6689e1c5,0xff34434b}));network.addView(force);form.addView(network);ui.gap(form,18);
        Switch adapt=new Switch(this);adapt.setText("自適應位元率");adapt.setTextColor(ConsoleStyle.INK);adapt.setChecked(adaptive);form.addView(adapt);Switch reconnect=new Switch(this);reconnect.setText("網絡變更後自動重連");reconnect.setTextColor(ConsoleStyle.INK);reconnect.setChecked(autoReconnect);form.addView(reconnect);ui.gap(form,12);
        TextView regionTitle=ui.text("中繼地區",12,ConsoleStyle.MUTED,false);form.addView(regionTitle);Spinner region=new Spinner(this);ArrayList<String> ids=new ArrayList<>(),names=new ArrayList<>();ids.add("auto");names.add("自動 · 故障時切換地區");JSONArray known=session==null?new JSONArray():session.relayRegions();for(int n=0;n<known.length();n++){JSONObject item=known.optJSONObject(n);if(item!=null){ids.add(item.optString("id"));names.add(item.optString("name"));}}if(!ids.contains(relayRegion)&&!relayRegion.equals("auto")){ids.add(relayRegion);names.add(relayRegion.equals("hk")?"香港":"日本");}region.setAdapter(new ArrayAdapter<String>(this,android.R.layout.simple_spinner_dropdown_item,names));region.setSelection(Math.max(0,ids.indexOf(relayRegion)));form.addView(region);ui.gap(form,18);TextView hint=ui.text("多人連線會沿用現有串流畫質。實際 FPS 視乎電話、畫面活動同網絡。",11,ConsoleStyle.MUTED,false);hint.setLineSpacing(dp(4),1);form.addView(hint);ui.gap(form,23);LinearLayout save=ui.primary(session!=null?"套用並重新連接":"儲存設定","check");form.addView(save);Dialog dialog=sheet("畫質與連線","揀一個適合你而家網絡嘅設定",form);save.setOnClickListener(v->{fps=f.value();maxSize=size.value();bitrate=rate.value();relay=force.isChecked();adaptive=adapt.isChecked();autoReconnect=reconnect.isChecked();relayRegion=ids.get(region.getSelectedItemPosition());persist();dialog.dismiss();if(session!=null)connect(currentUrl,sessionPin);else toast("設定已儲存");});dialog.show();}
    private void more(){LinearLayout content=ui.column();String[] labels={"畫質","全螢幕","旋轉","音量 +","音量 −","電源","剪貼簿","清晰畫面","重新連接","電話檔案","Root 終端","電話與中繼"},icons={"settings","fullscreen","rotate","volumeup","volumedown","power","copy","spark","refresh","copy","keyboard","network"};Dialog[] dialog=new Dialog[1];for(int row=0;row<4;row++){LinearLayout line=ui.row();for(int col=0;col<3;col++){int index=row*3+col;LinearLayout tile=ui.action(labels[index],icons[index],false);tile.setBackground(ui.ripple(ui.inset()));LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(0,dp(83),1);if(col>0)p.leftMargin=dp(9);line.addView(tile,p);tile.setOnClickListener(v->{afterSheetDismiss(dialog[0],()->{switch(index){case 0:settings();break;case 1:toggleFullscreen();break;case 2:if(ready&&session!=null)session.rotate();break;case 3:if(ready&&session!=null)session.key(24);break;case 4:if(ready&&session!=null)session.key(25);break;case 5:if(ready&&session!=null)session.key(26);break;case 6:if(ready&&session!=null)session.requestClipboard();break;case 7:if(session!=null)session.requestFrame();break;case 8:if(session!=null)connect(currentUrl,sessionPin);break;case 9:if(nativeTools!=null)nativeTools.files();break;case 10:if(nativeTools!=null)nativeTools.terminal();break;case 11:if(nativeTools!=null)nativeTools.dashboard();break;}});});}content.addView(line);if(row<3)ui.gap(content,9);}ui.gap(content,20);TextView end=ui.text("結束連線",13,0xffefa59c,true);end.setGravity(Gravity.CENTER);end.setBackground(ui.ripple(ui.fill(0x0cffffff,14)));content.addView(end,new LinearLayout.LayoutParams(-1,dp(48)));end.setOnClickListener(v->afterSheetDismiss(dialog[0],this::showDevices));dialog[0]=sheet("遙控工具","常用操作，隨手就到",content);dialog[0].show();}
    private void keyboard(){if(session==null||!ready){toast("請先連接電話。");return;}LinearLayout form=ui.column();EditText input=ui.input("輸入文字，支援中文同 Emoji",false);input.setSingleLine(false);input.setInputType(0x20001);input.setGravity(Gravity.TOP);form.addView(input,new LinearLayout.LayoutParams(-1,dp(140)));ui.gap(form,20);LinearLayout row=ui.row();TextView enter=ui.text("Enter ↵",13,ConsoleStyle.MINT,true);row.addView(enter,new LinearLayout.LayoutParams(0,dp(48),1));LinearLayout send=ui.primary("貼到電話","arrow");row.addView(send,new LinearLayout.LayoutParams(0,dp(48),1));form.addView(row);Dialog d=sheet("傳送文字","中文、Emoji，一樣貼到電話",form);send.setOnClickListener(v->{if(session!=null)session.paste(input.getText().toString());d.dismiss();});enter.setOnClickListener(v->{if(session!=null)session.key(66);});d.show();}
    private void basicAuth(){LinearLayout form=ui.column();EditText user=ui.input("外網帳戶",false),password=ui.input("外網密碼",false);password.setInputType(0x81);user.setText(basicUser);password.setText(basicPassword);form.addView(user);ui.gap(form,12);form.addView(password);ui.gap(form,14);EditText compatibility=ui.input("舊版 PIN（區域網相容模式，選填）",true);compatibility.setText(legacyPin);form.addView(compatibility);ui.gap(form,22);LinearLayout save=ui.primary("完成","check");form.addView(save);Dialog d=sheet("外網登入","只喺反向代理另設登入時填寫",form);save.setOnClickListener(v->{basicUser=user.getText().toString();basicPassword=password.getText().toString();legacyPin=compatibility.getText().toString().trim();d.dismiss();});d.show();}
    private void clipboardSheet(String value){LinearLayout form=ui.column();TextView text=ui.text(value,14,ConsoleStyle.INK,false);text.setTextIsSelectable(true);form.addView(text);ui.gap(form,22);LinearLayout copy=ui.primary("複製到控制端","copy");form.addView(copy);Dialog d=sheet("電話剪貼簿","由受控電話傳來嘅文字",form);copy.setOnClickListener(v->{((ClipboardManager)getSystemService(CLIPBOARD_SERVICE)).setPrimaryClip(ClipData.newPlainText("電話文字",value));d.dismiss();toast("已複製");});d.show();}
    Dialog openToolSheet(String title,String subtitle,LinearLayout content){Dialog d=sheet(title,subtitle,content);d.show();return d;}
    void pickUpload(){pickingDocument=true;Intent i=new Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*").addCategory(Intent.CATEGORY_OPENABLE);i.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION|Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);try{startActivityForResult(i,701);}catch(Exception e){pickingDocument=false;toast("未能開啟檔案選擇器");}}
    void saveDownload(String name){pickingDocument=true;Intent i=new Intent(Intent.ACTION_CREATE_DOCUMENT).setType("application/octet-stream").addCategory(Intent.CATEGORY_OPENABLE).putExtra(Intent.EXTRA_TITLE,name);try{startActivityForResult(i,702);}catch(Exception e){pickingDocument=false;toast("未能開啟儲存位置");}}
    @Override protected void onActivityResult(int request,int result,Intent data){super.onActivityResult(request,result,data);if(request==701||request==702){pickingDocument=false;if(result!=RESULT_OK||data==null||data.getData()==null||nativeTools==null)return;android.net.Uri uri=data.getData();if(request==701){try{getContentResolver().takePersistableUriPermission(uri,Intent.FLAG_GRANT_READ_URI_PERMISSION);}catch(SecurityException ignored){}nativeTools.upload(uri);}else nativeTools.save(uri);}}
    private void toast(String value){Toast.makeText(this,value,Toast.LENGTH_LONG).show();}
    @Override public void onBackPressed(){if(fullscreen)toggleFullscreen();else if(session!=null)showDevices();else super.onBackPressed();}
    @Override protected void onPause(){if(session!=null){session.releaseTouches();pointers.clear();}super.onPause();}
    @Override protected void onStop(){super.onStop();if(session!=null&&!pickingDocument&&!isChangingConfigurations())showDevices();}
    @Override protected void onDestroy(){
        closeSession();
        for(Dialog dialog:new ArrayList<>(openSheets)){dialog.setOnDismissListener(null);dialog.getWindow().setWindowAnimations(0);dialog.dismiss();}
        openSheets.clear();if(sheetScrim!=null){sheetScrim.animate().cancel();if(sheetScrim.getParent() instanceof ViewGroup)((ViewGroup)sheetScrim.getParent()).removeView(sheetScrim);sheetScrim=null;}
        super.onDestroy();
    }
}
