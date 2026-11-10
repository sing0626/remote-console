package com.sing.halfcloud.controller;
import org.junit.Test;
import static org.junit.Assert.*;
public class NetworkPolicyTest {
 @Test public void lossLowersRateAndRecoveryStaysWithinCeiling(){NetworkPolicy p=new NetworkPolicy();int rate=8000000;long rx=0,lost=0;
  p.sample(10000,0,0,20,0,rate,8000000);
  for(int i=1;i<=3;i++){rx+=100;lost+=10;int next=p.sample(10000+i*1000,rx,lost,600,90,rate,8000000);if(next>0)rate=next;}
  assertEquals(6000000,rate);
  for(int i=0;i<150;i++){rx+=100;int next=p.sample(30000+i*1000,rx,lost,30,2,rate,8000000);if(next>0)rate=next;}
  assertEquals(8000000,rate);
 }
 @Test public void idleScreenNeverMeansCongestion(){NetworkPolicy p=new NetworkPolicy();for(int i=0;i<100;i++)assertEquals(0,p.sample(10000+i*1000,0,0,1000,200,8000000,8000000));}
 @Test public void retryBackoffIsBounded(){assertEquals(2000,NetworkPolicy.retryDelay(1));assertEquals(4000,NetworkPolicy.retryDelay(2));assertEquals(30000,NetworkPolicy.retryDelay(100));}
}
