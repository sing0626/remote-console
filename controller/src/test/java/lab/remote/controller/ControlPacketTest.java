package lab.remote.controller;
import java.nio.*;
import java.util.Arrays;
public final class ControlPacketTest {
    static void check(boolean b){if(!b)throw new AssertionError();}
    public static void main(String[] args){
        check(Arrays.equals(ControlPacket.key(0,187),new byte[]{0,0,0,(byte)187}));
        check(Arrays.equals(ControlPacket.touch(1,3,123,456),new byte[]{2,1,3,0,123,1,(byte)200,0,0,0}));
        byte[] p=ControlPacket.clipboard("中文🙂");
        check(p[0]==9&&p[9]==1&&ByteBuffer.wrap(p,10,4).getInt()==10&&p.length==24);
        check(ControlPacket.position(0,50,100,100,100,200,false)==null);
        check(Arrays.equals(ControlPacket.position(50,50,100,100,100,200,false),new int[]{50,100}));
        check(Arrays.equals(ControlPacket.position(200,-10,100,100,100,200,true),new int[]{99,0}));
        check(ControlPacket.position(1,1,0,0,100,200,false)==null);
        System.out.println("Control protocol / Unicode / aspect-fit mapping passed");
    }
}
