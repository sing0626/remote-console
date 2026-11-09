package lab.remote.controller;

import android.content.Context;
import android.content.res.ColorStateList;
import android.graphics.*;
import android.graphics.drawable.*;
import android.view.*;
import android.view.accessibility.AccessibilityNodeInfo;
import android.widget.*;

/** Visual components shared by the native screens and tool sheets. */
final class ConsoleStyle {
    static final int BG=0xff09171f,INK=0xffedf6f4,MUTED=0xff91aab5,MINT=0xff9ae8ce;
    private final Context context;
    private final Typeface regular,bold;
    ConsoleStyle(Context context){this.context=context;regular=Typeface.createFromAsset(context.getAssets(),"fonts/NotoSansTC.ttf");bold=Typeface.create(regular,Typeface.BOLD);}
    int dp(float value){return Math.round(value*context.getResources().getDisplayMetrics().density);}
    LinearLayout column(){LinearLayout l=new LinearLayout(context);l.setOrientation(1);return l;}
    LinearLayout row(){LinearLayout l=new LinearLayout(context);l.setGravity(Gravity.CENTER_VERTICAL);return l;}
    TextView text(String label,int size,int color,boolean emphasized){TextView t=new TextView(context);t.setText(label);t.setTextSize(size);t.setTextColor(color);t.setTypeface(emphasized?bold:regular);t.setFontVariationSettings("'wght' "+(emphasized?600:400));t.setIncludeFontPadding(false);return t;}
    TextView text(String label,int size){return text(label,size,INK,false);}
    void gap(LinearLayout l,int size){l.addView(new View(context),new LinearLayout.LayoutParams(1,dp(size)));}
    Drawable panel(int radius){GradientDrawable d=new GradientDrawable(GradientDrawable.Orientation.TL_BR,new int[]{0xf21b3039,0xf0101f29});d.setCornerRadius(dp(radius));d.setStroke(dp(.7f),0x294b7277);return d;}
    Drawable sheetSurface(){GradientDrawable d=new GradientDrawable(GradientDrawable.Orientation.TL_BR,new int[]{0xff1b3039,0xff101f29});d.setCornerRadius(dp(28));d.setStroke(dp(.7f),0x294b7277);return d;}
    Drawable fill(int color,int radius){GradientDrawable d=new GradientDrawable();d.setColor(color);d.setCornerRadius(dp(radius));return d;}
    Drawable ripple(Drawable d){return new RippleDrawable(ColorStateList.valueOf(0x2489e1c5),d,null);}
    Drawable inset(){GradientDrawable d=new GradientDrawable();d.setColor(0x78101b24);d.setCornerRadius(dp(14));d.setStroke(dp(.7f),0x155b8892);return d;}
    ImageView icon(String name,int size,int color){ImageView v=new ImageView(context);v.setImageDrawable(new Glyph(name,color));v.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);v.setLayoutParams(new LinearLayout.LayoutParams(dp(size),dp(size)));return v;}
    ImageView brandMark(int size){ImageView v=new ImageView(context);v.setImageResource(lab.remote.controller.R.drawable.brand_mark);v.setBackground(fill(BG,Math.round(size*.3f)));v.setClipToOutline(true);v.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);v.setLayoutParams(new LinearLayout.LayoutParams(dp(size),dp(size)));return v;}
    ImageButton iconButton(String name,String label){ImageButton b=new ImageButton(context);b.setImageDrawable(new Glyph(name,INK));b.setContentDescription(label);b.setScaleType(ImageView.ScaleType.FIT_CENTER);b.setPadding(dp(10),dp(10),dp(10),dp(10));b.setBackground(ripple(fill(0x06ffffff,14)));b.setStateListAnimator(null);return b;}
    LinearLayout primary(String label,String glyph){LinearLayout l=row();l.setPadding(dp(18),0,dp(18),0);GradientDrawable d=new GradientDrawable(GradientDrawable.Orientation.TL_BR,new int[]{0xffa4ecd5,MINT});d.setCornerRadius(dp(16));d.setStroke(dp(.7f),0x669ef4d7);l.setBackground(ripple(d));l.setElevation(dp(3));l.addView(text(label,15,BG,true),new LinearLayout.LayoutParams(0,-2,1));l.addView(icon(glyph,20,BG));l.setClickable(true);l.setFocusable(true);l.setContentDescription(label);LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-1,dp(52));l.setLayoutParams(p);return l;}
    EditText input(String hint,boolean secret){EditText v=new EditText(context);v.setTextSize(14);v.setTextColor(INK);v.setHintTextColor(0xff6d8794);v.setHint(hint);v.setSingleLine(true);v.setPadding(dp(14),dp(10),dp(14),dp(10));v.setBackground(inset());v.setInputType(secret?0x12:android.text.InputType.TYPE_CLASS_TEXT|android.text.InputType.TYPE_TEXT_VARIATION_URI);v.setTypeface(regular);v.setFontVariationSettings("'wght' 400");v.setLayoutParams(new LinearLayout.LayoutParams(-1,dp(52)));return v;}
    LinearLayout label(String label,String glyph){LinearLayout l=row();l.addView(icon(glyph,12,MUTED));TextView t=text(label,10,MUTED,false);LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-2,-2);p.leftMargin=dp(6);l.addView(t,p);return l;}
    void field(LinearLayout parent,String label,String glyph,EditText input){parent.addView(label(label,glyph));gap(parent,8);parent.addView(input);}
    LinearLayout action(String label,String glyph,boolean selected){ActionCell l=new ActionCell(context);l.setOrientation(1);l.setGravity(Gravity.CENTER);l.setBackground(ripple(fill(selected?0x1689e1c5:Color.TRANSPARENT,18)));l.addView(icon(glyph,23,selected?MINT:0xffcadbdc));TextView title=text(label,9,selected?MINT:MUTED,false);LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-2,-2);p.topMargin=dp(6);l.addView(title,p);l.setContentDescription(label);l.setClickable(true);l.setFocusable(true);return l;}
    static final class ActionCell extends LinearLayout {
        ActionCell(Context c){super(c);}
        @Override public void onInitializeAccessibilityNodeInfo(AccessibilityNodeInfo info){super.onInitializeAccessibilityNodeInfo(info);info.setClassName(Button.class.getName());}
    }
    ChoiceGroup choices(int[] values,String[] labels,int value){return new ChoiceGroup(values,labels,value);}
    final class ChoiceGroup extends LinearLayout {
        private final int[] values;private int value;
        ChoiceGroup(int[] values,String[] labels,int selected){super(context);this.values=values;value=selected;for(int i=0;i<values.length;i++){final int n=i;TextView v=text(labels[i],12,INK,true);v.setGravity(Gravity.CENTER);v.setContentDescription(labels[i]);v.setClickable(true);v.setFocusable(true);LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(0,dp(43),1);if(i>0)p.leftMargin=dp(6);addView(v,p);v.setOnClickListener(w->{value=values[n];refresh();});}refresh();}
        int value(){return value;}
        private void refresh(){for(int i=0;i<getChildCount();i++){TextView v=(TextView)getChildAt(i);boolean active=values[i]==value;v.setTextColor(active?BG:MUTED);v.setBackground(ripple(fill(active?MINT:0x0cffffff,12)));v.setSelected(active);}}
    }
    Drawable backdrop(){return new Drawable(){final Paint p=new Paint(Paint.ANTI_ALIAS_FLAG);public void draw(Canvas c){Rect b=getBounds();c.drawColor(BG);p.setShader(new RadialGradient(b.width()*.05f,b.height()*.03f,b.width()*1.15f,new int[]{0xff16453f,0x0016453f},null,Shader.TileMode.CLAMP));c.drawRect(b,p);p.setShader(new RadialGradient(b.width(),b.height()*.35f,b.width()*.85f,new int[]{0x8f2b315e,0x002b315e},null,Shader.TileMode.CLAMP));c.drawRect(b,p);p.setShader(null);}public void setAlpha(int a){}public void setColorFilter(ColorFilter f){}public int getOpacity(){return PixelFormat.OPAQUE;}};}

    static final class Glyph extends Drawable {
        private final String name;private final Paint p=new Paint(3);private final int color;
        Glyph(String name,int color){this.name=name;this.color=color;}
        private void line(Canvas c,float... points){Path path=new Path();path.moveTo(points[0],points[1]);for(int i=2;i<points.length;i+=2)path.lineTo(points[i],points[i+1]);c.drawPath(path,p);}
        private void round(Canvas c,float l,float t,float r,float b,float rad){c.drawRoundRect(l,t,r,b,rad,rad,p);}
        @Override public void draw(Canvas c){Rect b=getBounds();c.save();float scale=Math.min(b.width(),b.height())/24f;c.translate(b.left+(b.width()-scale*24)/2,b.top+(b.height()-scale*24)/2);c.scale(scale,scale);p.setColor(color);p.setStyle(Paint.Style.STROKE);p.setStrokeWidth(1.65f);p.setStrokeCap(Paint.Cap.ROUND);p.setStrokeJoin(Paint.Join.ROUND);
            switch(name){
                case "back":line(c,14,5,7,12,14,19);break;
                case "arrow":line(c,6,18,18,6);line(c,8,6,18,6,18,16);break;
                case "home":line(c,3,10,12,3,21,10);line(c,5,9,5,21,10,21,10,14,14,14,14,21,19,21,19,9);break;
                case "recent":round(c,5,7,20,21,3);line(c,3,17,3,5,17,5);line(c,6,2,15,2);break;
                case "phone":round(c,7,2,17,22,3);line(c,10,5,14,5);line(c,11,19,13,19);break;
                case "keyboard":round(c,2,5,22,19,3);for(int y=9;y<=12;y+=3)for(int x=6;x<=18;x+=4)c.drawPoint(x,y,p);line(c,8,16,16,16);break;
                case "more":p.setStyle(Paint.Style.FILL);for(int x=5;x<=19;x+=7)c.drawCircle(x,12,1.6f,p);break;
                case "settings":line(c,4,6,20,6);line(c,4,12,20,12);line(c,4,18,20,18);c.drawCircle(9,6,2,p);c.drawCircle(15,12,2,p);c.drawCircle(10,18,2,p);break;
                case "mute":case "sound":case "volumeup":case "volumedown":line(c,4,9,8,9,13,5,13,19,8,15,4,15,4,9);if(name.equals("mute")){line(c,17,9,22,15);line(c,22,9,17,15);}else if(name.equals("sound")){c.drawArc(12,5,23,19,-65,130,false,p);c.drawArc(13,8,19,16,-65,130,false,p);}else{line(c,17,12,23,12);if(name.equals("volumeup"))line(c,20,9,20,15);}break;
                case "fullscreen":line(c,3,9,3,3,9,3);line(c,15,3,21,3,21,9);line(c,21,15,21,21,15,21);line(c,9,21,3,21,3,15);break;
                case "close":line(c,6,6,18,18);line(c,18,6,6,18);break;
                case "chevron":line(c,9,6,15,12,9,18);break;
                case "lock":round(c,5,10,19,22,3);c.drawArc(8,2,16,14,180,180,false,p);c.drawPoint(12,16,p);break;
                case "link":c.save();c.rotate(-40,12,12);round(c,3,8,13,16,4);round(c,11,8,21,16,4);line(c,9,12,15,12);c.restore();break;
                case "key":c.drawCircle(8,8,5,p);line(c,11.5f,11.5f,21,21);line(c,16,16,19,13);line(c,19,19,22,16);break;
                case "plus":line(c,5,12,19,12);line(c,12,5,12,19);break;
                case "refresh":c.drawArc(4,4,20,20,-50,290,false,p);line(c,15,3,20,4,19,9);break;
                case "rotate":c.drawArc(4,4,20,20,-65,285,false,p);line(c,16,2,21,4,19,9);break;
                case "power":c.drawArc(4,4,20,22,-50,280,false,p);line(c,12,2,12,11);break;
                case "copy":round(c,8,7,21,22,3);line(c,16,4,16,2,3,2,3,17,5,17);break;
                case "check":line(c,4,12,9,17,20,6);break;
                case "touch":c.drawCircle(12,7,4,p);line(c,10,13,10,7,13,7,13,13,17,12,20,15,18,21,10,21,5,16,7,14,10,16);break;
                case "wave":for(int x=4;x<=20;x+=4){float n=(x==12?8:x==8||x==16?5:2);line(c,x,12-n,x,12+n);}break;
                case "devices":round(c,3,3,17,16,3);line(c,7,20,21,20,21,7);break;
                case "spark":line(c,12,2,15,9,22,12,15,15,12,22,9,15,2,12,9,9,12,2);break;
                case "network":c.drawCircle(5,6,2,p);c.drawCircle(19,6,2,p);c.drawCircle(12,20,2,p);line(c,7,6,17,6);line(c,6,8,11,18);line(c,18,8,13,18);break;
            }c.restore();}
        @Override public void setAlpha(int alpha){p.setAlpha(alpha);invalidateSelf();} @Override public void setColorFilter(ColorFilter filter){p.setColorFilter(filter);invalidateSelf();} @Override public int getOpacity(){return PixelFormat.TRANSLUCENT;}
        @Override public int getIntrinsicWidth(){return 24;}@Override public int getIntrinsicHeight(){return 24;}
    }
}
