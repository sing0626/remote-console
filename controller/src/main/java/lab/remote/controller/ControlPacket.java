package lab.remote.controller;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;

/** Existing host's binary protocol, independent of Android and UI. */
public final class ControlPacket {
    private static ByteBuffer buffer(int size) { return ByteBuffer.allocate(size).order(ByteOrder.BIG_ENDIAN); }
    public static byte[] key(int action, int code) { return buffer(4).put((byte)0).put((byte)action).putShort((short)code).array(); }
    public static byte[] touch(int action, int id, int x, int y) {
        if (id < 0 || id > 9 || action < 0 || action > 2) throw new IllegalArgumentException("Invalid touch");
        return buffer(10).put((byte)2).put((byte)action).put((byte)id)
            .putShort((short)Math.max(0,Math.min(65535,x))).putShort((short)Math.max(0,Math.min(65535,y)))
            .putShort((short)(action==1?0:65535)).put((byte)(action==1?0:1)).array();
    }
    public static byte[] clipboard(String value) {
        byte[] bytes=value.getBytes(StandardCharsets.UTF_8);
        if(bytes.length>60000) throw new IllegalArgumentException("文字過長，請分段傳送。");
        return buffer(14+bytes.length).put((byte)9).putLong(0).put((byte)1).putInt(bytes.length).put(bytes).array();
    }
    /** Map a point through aspect-fit letterboxing. Null means a down outside the video. */
    public static int[] position(float x,float y,int viewW,int viewH,int videoW,int videoH,boolean clamp) {
        if(viewW<=0||viewH<=0||videoW<=0||videoH<=0)return null;
        double scale=Math.min((double)viewW/videoW,(double)viewH/videoH);
        double left=(viewW-videoW*scale)/2,top=(viewH-videoH*scale)/2;
        if(!clamp&&(x<left||y<top||x>=left+videoW*scale||y>=top+videoH*scale))return null;
        return new int[]{Math.max(0,Math.min(videoW-1,(int)((x-left)/scale))),Math.max(0,Math.min(videoH-1,(int)((y-top)/scale)))};
    }
}
