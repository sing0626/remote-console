package lab.remote.controller;

import android.content.Context;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.security.MessageDigest;
import java.util.Arrays;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** A gateway credential is protected by an app-specific Android Keystore key.
 * The server origin is authenticated as associated data, so stored values
 * cannot be substituted between gateways. No phone PIN is persisted here. */
final class ControllerCredentialStore {
    private static final String ALIAS="RemoteController.TrustedClient.v1";
    private final SharedPreferences preferences;
    ControllerCredentialStore(Context context){preferences=context.getSharedPreferences("trusted_controllers",Context.MODE_PRIVATE);}
    private SecretKey key()throws Exception {
        KeyStore store=KeyStore.getInstance("AndroidKeyStore");store.load(null);
        if(store.containsAlias(ALIAS))return (SecretKey)store.getKey(ALIAS,null);
        KeyGenerator generator=KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore");
        generator.init(new KeyGenParameterSpec.Builder(ALIAS,KeyProperties.PURPOSE_ENCRYPT|KeyProperties.PURPOSE_DECRYPT).setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).setRandomizedEncryptionRequired(true).build());
        return generator.generateKey();
    }
    private String index(String origin)throws Exception{return Base64.encodeToString(MessageDigest.getInstance("SHA-256").digest(origin.getBytes(StandardCharsets.UTF_8)),Base64.NO_WRAP|Base64.URL_SAFE);}
    String load(String origin)throws Exception {
        String saved=preferences.getString(index(origin),null);if(saved==null)return null;
        byte[] data=Base64.decode(saved,Base64.NO_WRAP);if(data.length<29||data.length>2048)throw new IllegalStateException("Invalid protected credential");
        Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.DECRYPT_MODE,key(),new GCMParameterSpec(128,Arrays.copyOfRange(data,0,12)));cipher.updateAAD(origin.getBytes(StandardCharsets.UTF_8));
        return new String(cipher.doFinal(data,12,data.length-12),StandardCharsets.UTF_8);
    }
    void save(String origin,String credential)throws Exception {
        Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.ENCRYPT_MODE,key());cipher.updateAAD(origin.getBytes(StandardCharsets.UTF_8));
        byte[] encrypted=cipher.doFinal(credential.getBytes(StandardCharsets.UTF_8)),iv=cipher.getIV(),data=new byte[iv.length+encrypted.length];System.arraycopy(iv,0,data,0,iv.length);System.arraycopy(encrypted,0,data,iv.length,encrypted.length);
        if(iv.length!=12||!preferences.edit().putString(index(origin),Base64.encodeToString(data,Base64.NO_WRAP)).commit())throw new IllegalStateException("Credential was not saved");
    }
    void remove(String origin)throws Exception {if(!preferences.edit().remove(index(origin)).commit())throw new IllegalStateException("Credential was not removed");}
}
