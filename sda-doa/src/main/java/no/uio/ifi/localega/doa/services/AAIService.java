package no.uio.ifi.localega.doa.services;

import com.google.gson.Gson;
import com.google.gson.JsonObject;
import lombok.extern.slf4j.Slf4j;
import no.elixir.clearinghouse.Clearinghouse;
import no.elixir.clearinghouse.model.Visa;
import no.elixir.clearinghouse.model.VisaType;
import org.apache.commons.lang3.StringUtils;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.util.CollectionUtils;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.*;
import java.util.stream.Collectors;

/**
 * Service for extracting datasets access information from the JWT token.
 */
@Slf4j
@Service
public class AAIService {

    @Value("${ga4gh.passport.userinfo-endpoint-url}")
    private String userInfoEndpointURL;

    @Value("${ga4gh.passport.openid-configuration-url}")
    private String openIDConfigurationURL;

    @Value("${ga4gh.passport.public-key-path}")
    private String passportPublicKeyPath;

    @Value("${ga4gh.visa.public-key-path}")
    private String visaPublicKeyPath;

    /**
     * Extracts datasets access information from the JWT or opaque access token.
     *
     * @param accessToken JWT or opaque access token.
     * @return IDs of datasets user has access to.
     */
    public Collection<String> getDatasetIds(String accessToken) {
        Collection<Visa> visas = new ArrayList<>();
        if (StringUtils.countMatches(accessToken, '.') == 2) { 
            var tokenArray = accessToken.split("[.]");
            byte[] decodedPayload = Base64.getUrlDecoder().decode(tokenArray[1]);
            String decodedPayloadString = new String(decodedPayload);
            Gson gson = new Gson();
            Set<String> claims = gson.fromJson(decodedPayloadString, JsonObject.class).keySet();

            boolean isVisa = claims.contains("ga4gh_visa_v1");
            if (isVisa) {
                getVisa(accessToken).ifPresent(visas::add);
            } else {
                visas.addAll(getVisasFromJWTToken(accessToken));
            }
        } else { // opaque token
            visas = getVisasFromOpaqueToken(accessToken);
        }

        List<Visa> controlledAccessGrantsVisas = visas
                .stream()
                .filter(v -> v.getType().equalsIgnoreCase(VisaType.ControlledAccessGrants.name()))
                .collect(Collectors.toList());

        if (CollectionUtils.isEmpty(controlledAccessGrantsVisas)) {
            log.info("Unauthorized access attempt: user doesn't have any valid visas.");
        }

        String subject = controlledAccessGrantsVisas.stream().findFirst().orElseThrow(RuntimeException::new).getSub();

        log.info("Authentication and authorization attempt. User {} provided following valid GA4GH Visas: {}", subject, controlledAccessGrantsVisas);
        Set<String> datasets = controlledAccessGrantsVisas
                .stream()
                .map(Visa::getValue)
                .map(d -> StringUtils.stripEnd(d, "/"))
                .map(d -> StringUtils.substringAfterLast(d, "/"))
                .collect(Collectors.toSet());
        log.info("User has access to the following datasets: {}", datasets);
        return datasets;
    }

    protected Collection<Visa> getVisasFromOpaqueToken(String accessToken) {
        return Clearinghouse.INSTANCE.getVisaTokensFromOpaqueToken(accessToken, userInfoEndpointURL)
                .stream()
                .map(this::getVisa)
                .filter(Optional::isPresent)
                .map(Optional::get)
                .collect(Collectors.toList());
    }

    protected Collection<Visa> getVisasFromJWTToken(String accessToken) {
        Collection<String> visaTokens;
        try {
            String passportPublicKey = Files.readString(Path.of(passportPublicKeyPath));
            visaTokens = Clearinghouse.INSTANCE.getVisaTokensWithPEMPublicKey(accessToken, passportPublicKey);
        } catch (IOException e) {
            visaTokens = Clearinghouse.INSTANCE.getVisaTokens(accessToken, openIDConfigurationURL);
        }
        return visaTokens
                .stream()
                .map(this::getVisa)
                .filter(Optional::isPresent)
                .map(Optional::get)
                .collect(Collectors.toList());
    }

    protected Optional<Visa> getVisa(String visaToken) {
        try {
            String visaPublicKey = Files.readString(Path.of(visaPublicKeyPath));
            return Clearinghouse.INSTANCE.getVisaWithPEMPublicKey(visaToken, visaPublicKey);
        } catch (IOException e) {
            return Clearinghouse.INSTANCE.getVisa(visaToken);
        }
    }


}
