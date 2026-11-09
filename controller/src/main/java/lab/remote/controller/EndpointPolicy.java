package lab.remote.controller;

import java.util.Locale;

final class EndpointPolicy {
    static boolean isPrivateHost(String host) {
        if (host == null) return false;
        String h = host.toLowerCase(Locale.ROOT);
        if (h.equals("localhost") || h.equals("::1") || h.endsWith(".local")) return true;
        String[] parts = h.split("\\.", -1);
        if (parts.length != 4) return false;
        int[] numbers = new int[4];
        for (int i=0; i<4; i++) {
            String part=parts[i];
            if (!part.matches("[0-9]{1,3}") || (part.length()>1 && part.charAt(0)=='0')) return false;
            numbers[i]=Integer.parseInt(part);
            if (numbers[i]>255) return false;
        }
        return numbers[0]==10 || numbers[0]==127 || (numbers[0]==192 && numbers[1]==168)
            || (numbers[0]==172 && numbers[1]>=16 && numbers[1]<=31);
    }
}
