package com.sing.halfcloud.controller;
/** Deterministic network policy, independent of UI/WebRTC callbacks. */
final class NetworkPolicy {
    private long received=-1,lost=-1,lastChange;
    private int bad,good;
    static long retryDelay(int attempt){return Math.min(30000L,2000L<<Math.min(4,Math.max(0,attempt-1)));}
    int sample(long now,long packets,long losses,double rttMs,double jitterMs,int current,int ceiling){
        if(received<0||packets<received){received=packets;lost=losses;return 0;}
        long rx=packets-received,drop=Math.max(0,losses-lost);received=packets;lost=losses;
        if(rx+drop<20){bad=good=0;return 0;}
        double fraction=(double)drop/(rx+drop);
        if(fraction>.04||rttMs>450||jitterMs>80){bad++;good=0;}else if(fraction<.01&&rttMs<220&&jitterMs<35){good++;bad=0;}else{bad=good=0;}
        if(now-lastChange<7000)return 0;
        int next=current;
        if(bad>=3){next=Math.max(Math.min(1000000,ceiling),current*3/4);bad=good=0;}
        else if(good>=15){next=Math.min(ceiling,current+Math.max(250000,current/10));bad=good=0;}
        if(next==current)return 0;lastChange=now;return next;
    }
    void reset(){received=lost=-1;bad=good=0;lastChange=0;}
}
