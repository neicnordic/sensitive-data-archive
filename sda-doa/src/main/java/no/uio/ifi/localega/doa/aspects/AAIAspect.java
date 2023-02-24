package no.uio.ifi.localega.doa.aspects;

import lombok.extern.slf4j.Slf4j;
import no.uio.ifi.localega.doa.services.AAIService;
import org.aspectj.lang.ProceedingJoinPoint;
import org.aspectj.lang.annotation.Around;
import org.aspectj.lang.annotation.Aspect;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;

import javax.servlet.http.HttpServletRequest;

/**
 * AOP aspect that handles authentication and authorization.
 */
@Slf4j
@Aspect
@Component
public class AAIAspect {

    public static final String DATASETS = "datasets";

    @Autowired
    protected AAIService aaiService;

    @Autowired
    protected HttpServletRequest request;

    /**
     * Retrieves GA4GH Visas from the JWT token provided. Decides on whether to allow the request or not.
     *
     * @param joinPoint Join point referencing proxied method.
     * @return Either the object, returned by the proxied method, or HTTP error response.
     * @throws Throwable In case of error.
     */
    @Around("execution(public * no.uio.ifi.localega.doa.rest.*.*(..))")
    public Object authenticate(ProceedingJoinPoint joinPoint) throws Throwable {
        try {
            request.setAttribute(DATASETS, aaiService.getDatasetIds(getJWTToken()));
            return joinPoint.proceed();
        } catch (Exception e) {
            log.info(e.getMessage(), e);
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).body(e.getMessage());
        }
    }

    protected String getJWTToken() {
        return request.getHeader(HttpHeaders.AUTHORIZATION).replace("Bearer ", "");
    }

}
